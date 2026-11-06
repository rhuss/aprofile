#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pillow", "pyyaml"]
# ///
"""Overlap gate for Excalidraw diagrams (FR-023), the claims gate (FR-044), and
native-resolution crops (FR-023a).

check: parses the .excalidraw JSON, samples every arrow path (Catmull-Rom through the
points for curved arrows, the polyline otherwise) and reports
  arrow-crosses-text    an arrow passes through a free text or another arrow's label
  arrow-crosses-box     an arrow passes through a box it is not bound to
  crosses-frame-border  a box or label straddles a frame border (frames, or groups
                        whose largest rectangle encloses another box)
  duplicate-bound-text  a container has more than one bound text
  unbound-arrow-end     an arrow end is not bound to an existing element
Export is blocked until it reports zero findings.

claims: checks that a claims file covers what the diagram asserts. Every text (box
labels, arrow labels, free text), every arrow, and every frame must be named by a
claim, and every claim except a legend claim must cite a source. It reports
  claim-missing-label   a text no claim names (by its id or its container's id)
  claim-missing-arrow   an arrow no `connects` claim names
  claim-missing-frame   a frame no `contains` claim names
  claim-no-source       a claim without a well-formed source
  claim-unknown-element a claim names an element the scene does not have
Whether the sources support the claims is the reviewers' judgment, not this gate's.

crops: cuts an exported PNG into tiles of at most 1600 x 1200 px at native resolution,
plus one crop around each finding, for the controller's review.

Usage:
  uv run tools/guide/diagram_check.py check SCENE [--json]
      exit 0 clean, 1 findings (text lines, then one JSON line), 2 unreadable scene
  uv run tools/guide/diagram_check.py claims SCENE --claims CLAIMS.yaml [--json]
      same exit codes; 2 also for an unreadable claims file
  uv run tools/guide/diagram_check.py crops PNG --scene SCENE --out DIR [--padding 10]
"""
from __future__ import annotations

import argparse
import json
import math
import re
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path

BOX_TYPES = {"rectangle", "ellipse", "diamond", "image"}
ARROW_TYPES = {"arrow"}
LINEAR_TYPES = {"arrow", "line"}
TILE_W, TILE_H = 1600, 1200
EPS = 0.5

BBox = tuple[float, float, float, float]  # x1, y1, x2, y2
Point = tuple[float, float]


@dataclass
class Element:
    id: str
    type: str
    x: float
    y: float
    width: float
    height: float
    angle: float = 0.0
    raw: dict = field(default_factory=dict, repr=False)
    points: list[Point] = field(default_factory=list)  # absolute, linear elements only
    bbox: BBox = (0.0, 0.0, 0.0, 0.0)

    @property
    def container_id(self) -> str | None:
        return self.raw.get("containerId")

    @property
    def group_ids(self) -> list[str]:
        return list(self.raw.get("groupIds") or [])

    @property
    def bound_elements(self) -> list[dict]:
        return list(self.raw.get("boundElements") or [])

    @property
    def curved(self) -> bool:
        r = self.raw.get("roundness")
        return isinstance(r, dict) and r.get("type") == 2 and len(self.points) >= 3


@dataclass
class Scene:
    elements: dict[str, Element]
    frames: list[Element]
    bounds: BBox
    path: Path | None = None


@dataclass
class Finding:
    kind: str
    element_ids: list[str]
    bbox: BBox
    message: str

    def to_dict(self) -> dict:
        d = asdict(self)
        d["bbox"] = [round(v, 1) for v in self.bbox]
        return d


# --- geometry -------------------------------------------------------------------


def _rotate(p: Point, c: Point, angle: float) -> Point:
    if not angle:
        return p
    s, co = math.sin(angle), math.cos(angle)
    dx, dy = p[0] - c[0], p[1] - c[1]
    return (c[0] + dx * co - dy * s, c[1] + dx * s + dy * co)


def _aabb(points: list[Point]) -> BBox:
    xs = [p[0] for p in points]
    ys = [p[1] for p in points]
    return (min(xs), min(ys), max(xs), max(ys))


def _inside(p: Point, b: BBox, tol: float = 0.0) -> bool:
    return b[0] - tol <= p[0] <= b[2] + tol and b[1] - tol <= p[1] <= b[3] + tol


def _contains(outer: BBox, inner: BBox) -> bool:
    return (outer[0] - EPS <= inner[0] and outer[1] - EPS <= inner[1]
            and inner[2] <= outer[2] + EPS and inner[3] <= outer[3] + EPS)


def _overlaps(a: BBox, b: BBox) -> bool:
    return a[0] < b[2] - EPS and b[0] < a[2] - EPS and a[1] < b[3] - EPS and b[1] < a[3] - EPS


def _union(boxes: list[BBox]) -> BBox:
    return (min(b[0] for b in boxes), min(b[1] for b in boxes),
            max(b[2] for b in boxes), max(b[3] for b in boxes))


def _area(b: BBox) -> float:
    return max(0.0, b[2] - b[0]) * max(0.0, b[3] - b[1])


def _catmull_rom(p0: Point, p1: Point, p2: Point, p3: Point, t: float) -> Point:
    t2, t3 = t * t, t * t * t
    return tuple(
        0.5 * (2 * p1[i] + (-p0[i] + p2[i]) * t + (2 * p0[i] - 5 * p1[i] + 4 * p2[i] - p3[i]) * t2
               + (-p0[i] + 3 * p1[i] - 3 * p2[i] + p3[i]) * t3)
        for i in (0, 1)
    )


def sample_arrow(element: Element, step_px: float = 4) -> list[Point]:
    """Points along the arrow path, about step_px apart (absolute scene coordinates)."""
    pts = element.points
    if len(pts) < 2:
        return list(pts)
    out: list[Point] = [pts[0]]
    if element.curved:
        ext = [pts[0], *pts, pts[-1]]
        for i in range(1, len(ext) - 2):
            p0, p1, p2, p3 = ext[i - 1], ext[i], ext[i + 1], ext[i + 2]
            # estimate the segment length from a fine pre-sampling, then sample evenly in t
            fine = [_catmull_rom(p0, p1, p2, p3, k / 32) for k in range(33)]
            length = sum(math.dist(a, b) for a, b in zip(fine, fine[1:]))
            n = max(1, math.ceil(length / step_px * 1.15))
            out.extend(_catmull_rom(p0, p1, p2, p3, k / n) for k in range(1, n + 1))
    else:
        for a, b in zip(pts, pts[1:]):
            n = max(1, math.ceil(math.dist(a, b) / step_px))
            out.extend((a[0] + (b[0] - a[0]) * k / n, a[1] + (b[1] - a[1]) * k / n) for k in range(1, n + 1))
    return out


# --- loading --------------------------------------------------------------------


def _element(raw: dict) -> Element:
    el = Element(id=str(raw["id"]), type=str(raw.get("type", "")), x=float(raw.get("x", 0)),
                 y=float(raw.get("y", 0)), width=float(raw.get("width") or 0),
                 height=float(raw.get("height") or 0), angle=float(raw.get("angle") or 0), raw=raw)
    if el.type in LINEAR_TYPES:
        rel = [(float(p[0]), float(p[1])) for p in raw.get("points") or [[0, 0]]]
        local = [(el.x + px, el.y + py) for px, py in rel]
        if el.angle:
            lb = _aabb(local)
            c = ((lb[0] + lb[2]) / 2, (lb[1] + lb[3]) / 2)
            local = [_rotate(p, c, el.angle) for p in local]
        el.points = local
        el.bbox = _aabb(sample_arrow(el) if len(local) > 1 else local)
    else:
        corners = [(el.x, el.y), (el.x + el.width, el.y), (el.x + el.width, el.y + el.height),
                   (el.x, el.y + el.height)]
        c = (el.x + el.width / 2, el.y + el.height / 2)
        el.bbox = _aabb([_rotate(p, c, el.angle) for p in corners])
    return el


def _group_frames(elements: dict[str, Element]) -> list[Element]:
    groups: dict[str, list[Element]] = {}
    for el in elements.values():
        for gid in el.group_ids:
            groups.setdefault(gid, []).append(el)
    frames: dict[str, Element] = {}
    for members in groups.values():
        rects = [m for m in members if m.type == "rectangle"]
        if not rects:
            continue
        frame = max(rects, key=lambda m: _area(m.bbox))
        if any(m is not frame and m.type in BOX_TYPES and _contains(frame.bbox, m.bbox) for m in members):
            frames[frame.id] = frame
    return list(frames.values())


def scene_from_dict(data: dict, path: Path | None = None) -> Scene:
    elements = {}
    for raw in data.get("elements") or []:
        if raw.get("isDeleted"):
            continue
        el = _element(raw)
        elements[el.id] = el
    frames = [el for el in elements.values() if el.type in ("frame", "magicframe")] + _group_frames(elements)
    bounds = _union([el.bbox for el in elements.values()]) if elements else (0.0, 0.0, 0.0, 0.0)
    return Scene(elements=elements, frames=frames, bounds=bounds, path=path)


def load_scene(path: Path | str) -> Scene:
    path = Path(path)
    return scene_from_dict(json.loads(path.read_text()), path)


# --- checks ---------------------------------------------------------------------


def _binding(arrow: Element, end: str) -> str | None:
    raw = arrow.raw
    b = raw.get(f"{end}Binding")
    if isinstance(b, dict) and b.get("elementId"):
        return str(b["elementId"])
    for key in (f"{end}ElementId", end):
        v = raw.get(key)
        if isinstance(v, str) and v:
            return v
        if isinstance(v, dict) and v.get("id"):
            return str(v["id"])
    return None


def check(scene: Scene) -> list[Finding]:
    els = scene.elements
    frame_ids = {f.id for f in scene.frames}
    findings: list[Finding] = []

    boxes = [e for e in els.values() if e.type in BOX_TYPES and e.id not in frame_ids]
    texts = [e for e in els.values() if e.type == "text"]

    def is_box_label(t: Element) -> bool:
        c = els.get(t.container_id) if t.container_id else None
        return c is not None and c.type not in LINEAR_TYPES

    for arrow in (e for e in els.values() if e.type in ARROW_TYPES):
        bound = {i for i in (_binding(arrow, "start"), _binding(arrow, "end")) if i}
        bound |= {e.id for e in els.values() if any(b.get("id") == arrow.id for b in e.bound_elements)}
        own_labels = {t.id for t in texts if t.container_id == arrow.id}
        excluded = [els[i].bbox for i in bound | own_labels if i in els]
        samples = [p for p in sample_arrow(arrow) if not any(_inside(p, b, 1.0) for b in excluded)]

        for box in boxes:
            if box.id in bound:
                continue
            hits = [p for p in samples if _inside(p, box.bbox)]
            if hits:
                findings.append(Finding("arrow-crosses-box", [arrow.id, box.id], _union([box.bbox, _aabb(hits)]),
                                        f"arrow {arrow.id} crosses box {box.id} it is not bound to"))
        for text in texts:
            if text.id in own_labels or is_box_label(text):
                continue
            hits = [p for p in samples if _inside(p, text.bbox)]
            if hits:
                what = "the label of arrow " + text.container_id if text.container_id else "text"
                findings.append(Finding("arrow-crosses-text", [arrow.id, text.id], _union([text.bbox, _aabb(hits)]),
                                        f"arrow {arrow.id} crosses {what} {text.id} ({_snippet(text)})"))

        for end in ("start", "end"):
            target = _binding(arrow, end)
            if target is None or target not in els:
                p = arrow.points[0] if end == "start" else arrow.points[-1]
                why = "not bound" if target is None else f"bound to missing element {target}"
                findings.append(Finding("unbound-arrow-end", [arrow.id], (p[0] - 10, p[1] - 10, p[0] + 10, p[1] + 10),
                                        f"{end} of arrow {arrow.id} is {why}"))

    labels = [t for t in texts if not is_box_label(t)]
    for frame in scene.frames:
        for el in boxes + labels + [f for f in scene.frames if f is not frame]:
            if el.id == frame.id or not _overlaps(el.bbox, frame.bbox) or _contains(frame.bbox, el.bbox):
                continue
            if _contains(el.bbox, frame.bbox):
                continue  # an outer frame around this one
            findings.append(Finding("crosses-frame-border", [el.id, frame.id], _union([el.bbox]),
                                    f"{el.type} {el.id} crosses the border of frame {frame.id}"))

    containers: dict[str, list[str]] = {}
    for t in texts:
        if t.container_id:
            containers.setdefault(t.container_id, []).append(t.id)
    for cid, tids in containers.items():
        if len(tids) > 1:
            box = els[cid].bbox if cid in els else _union([els[t].bbox for t in tids])
            findings.append(Finding("duplicate-bound-text", [cid, *tids], box,
                                    f"container {cid} has {len(tids)} bound texts ({', '.join(tids)}); keep one"))
    return findings


# --- claims ---------------------------------------------------------------------

CLAIM_KINDS = {"label", "contains", "connects", "legend"}
SOURCE_RE = re.compile(r"^(?:[a-z0-9][a-z0-9._-]*:[^\s:]+(?::\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*)?|url:\S+|live:\S.*)$")


def load_claims(path: Path | str) -> list[dict]:
    import yaml

    try:
        data = yaml.safe_load(Path(path).read_text()) or {}
    except yaml.YAMLError as err:
        raise ValueError(f"not valid YAML: {err}") from err
    claims = data.get("claims") if isinstance(data, dict) else None
    if not isinstance(claims, list):
        raise ValueError("claims file has no `claims` list")
    return [c for c in claims if isinstance(c, dict)]


def check_claims(scene: Scene, claims: list[dict]) -> list[Finding]:
    els = scene.elements
    findings: list[Finding] = []
    named: dict[str, set[str]] = {k: set() for k in CLAIM_KINDS}

    for i, claim in enumerate(claims, 1):
        cid = str(claim.get("id") or f"#{i}")
        kind = str(claim.get("kind") or "")
        ids = [str(e) for e in claim.get("elements") or []]
        for eid in ids:
            if eid not in els:
                findings.append(Finding("claim-unknown-element", [eid], scene.bounds,
                                        f"claim {cid} names element {eid}, which the scene does not have"))
        if kind not in CLAIM_KINDS:
            findings.append(Finding("claim-no-source", ids, scene.bounds,
                                    f"claim {cid} has kind {kind!r}; use one of {sorted(CLAIM_KINDS)}"))
            continue
        named[kind].update(ids)
        if kind == "legend":
            continue
        sources = [str(s).strip() for s in claim.get("sources") or []]
        if not sources or not all(SOURCE_RE.match(s) for s in sources):
            bad = [s for s in sources if not SOURCE_RE.match(s)]
            why = f"malformed source {bad[0]!r}" if bad else "no source"
            findings.append(Finding("claim-no-source", ids, scene.bounds, f"claim {cid} ({kind}) has {why}"))

    any_claim = set().union(*named.values())
    for el in els.values():
        if el.type == "text" and el.id not in any_claim and (el.container_id or "") not in any_claim:
            findings.append(Finding("claim-missing-label", [el.id], el.bbox,
                                    f"no claim names text {el.id} ({_snippet(el)})"))
        elif el.type in ARROW_TYPES and el.id not in named["connects"]:
            findings.append(Finding("claim-missing-arrow", [el.id], el.bbox,
                                    f"no connects claim names arrow {el.id}"))
    for frame in scene.frames:
        if frame.id not in named["contains"]:
            findings.append(Finding("claim-missing-frame", [frame.id], frame.bbox,
                                    f"no contains claim names frame {frame.id}"))
    return findings


def _snippet(text: Element) -> str:
    s = str(text.raw.get("text") or "").replace("\n", " ")
    return repr(s[:30] + ("..." if len(s) > 30 else ""))


# --- crops ----------------------------------------------------------------------


def to_pixels(bbox: BBox, scene: Scene, png_size: tuple[int, int], padding: float = 10) -> tuple[int, int, int, int]:
    """Map a scene bounding box to pixel coordinates in an export of the whole scene."""
    x0, y0, x1, _ = scene.bounds
    scale = png_size[0] / ((x1 - x0) + 2 * padding)
    return (round((bbox[0] - x0 + padding) * scale), round((bbox[1] - y0 + padding) * scale),
            round((bbox[2] - x0 + padding) * scale), round((bbox[3] - y0 + padding) * scale))


def crops(png: Path, scene: Scene, out_dir: Path, findings: list[Finding], padding: float = 10) -> list[Path]:
    """Write native-resolution tiles and one crop per finding; return the written paths."""
    from PIL import Image

    out_dir = Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)
    img = Image.open(png)
    w, h = img.size
    written: list[Path] = []
    for r in range(math.ceil(h / TILE_H)):
        for c in range(math.ceil(w / TILE_W)):
            box = (c * TILE_W, r * TILE_H, min(w, (c + 1) * TILE_W), min(h, (r + 1) * TILE_H))
            path = out_dir / f"tile-r{r}-c{c}.png"
            img.crop(box).save(path)
            written.append(path)
    scale = w / ((scene.bounds[2] - scene.bounds[0]) + 2 * padding)
    margin = max(20, round(20 * scale))
    for i, f in enumerate(findings, 1):
        x1, y1, x2, y2 = to_pixels(f.bbox, scene, (w, h), padding)
        box = (max(0, x1 - margin), max(0, y1 - margin), min(w, x2 + margin), min(h, y2 + margin))
        if box[2] <= box[0] or box[3] <= box[1]:
            continue
        path = out_dir / f"finding-{i:02d}-{f.kind}.png"
        img.crop(box).save(path)
        written.append(path)
    return written


# --- CLI ------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Overlap gate and review crops for Excalidraw diagrams")
    sub = parser.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check", help="report overlaps; exit 1 on findings")
    c.add_argument("scene", type=Path)
    c.add_argument("--json", action="store_true", help="print only the JSON report")
    m = sub.add_parser("claims", help="check that a claims file covers every label, arrow, and frame; exit 1 on findings")
    m.add_argument("scene", type=Path)
    m.add_argument("--claims", type=Path, required=True)
    m.add_argument("--json", action="store_true", help="print only the JSON report")
    k = sub.add_parser("crops", help="cut an exported PNG into review tiles and finding crops")
    k.add_argument("png", type=Path)
    k.add_argument("--scene", type=Path, required=True)
    k.add_argument("--out", type=Path, required=True)
    k.add_argument("--padding", type=float, default=10, help="export padding in scene units (default 10)")
    args = parser.parse_args(argv)

    try:
        scene = load_scene(args.scene)
    except (OSError, ValueError, KeyError) as err:
        print(f"cannot read scene {args.scene}: {err}", file=sys.stderr)
        return 2
    if args.cmd == "claims":
        try:
            claims = load_claims(args.claims)
        except (OSError, ValueError) as err:
            print(f"cannot read claims {args.claims}: {err}", file=sys.stderr)
            return 2
        findings = check_claims(scene, claims)
    else:
        findings = check(scene)

    if args.cmd in ("check", "claims"):
        report = {"scene": str(args.scene), "count": len(findings), "findings": [f.to_dict() for f in findings]}
        if not args.json:
            for f in findings:
                print(f"{f.kind}: {f.message} [{', '.join(f.element_ids)}]")
            print(f"diagram_check: {len(findings)} finding(s) in {args.scene}")
        print(json.dumps(report))
        return 1 if findings else 0

    try:
        paths = crops(args.png, scene, args.out, findings, args.padding)
    except OSError as err:
        print(f"cannot read image {args.png}: {err}", file=sys.stderr)
        return 2
    for p in paths:
        print(f"wrote {p}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
