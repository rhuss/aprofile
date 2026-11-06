#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Load and validate a guide project's configuration files.

  guide.yaml     project configuration (format, personas, sections, internal part,
                 optional Antora branding block)
  baseline.yaml  pinned sources and change requests
  inputs.yaml    input register (references only, never raw content)

Usage: uv run tools/guide/guidecfg.py validate [--root DIR]
         exits 1 and prints one `file:key: message` line per problem; the branding
         block and the overlay directory ui/supplemental/ are checked by
         tools/guide/branding.py (its lines name their own location, for example
         `ui/supplemental:css/x.css: ...`), and its warnings print as
         `location: warning: ...` without changing the exit status
       uv run tools/guide/guidecfg.py get KEY [--root DIR]
         prints one value: title, slug, format, rigor, internal.enabled,
         drift.schedule, or baseline.captured
"""
from __future__ import annotations

import argparse
import datetime as _dt
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import yaml

import branding

FORMATS = ("antora", "mkdocs", "gfm")
RIGORS = ("fast", "standard", "strict")
SOURCE_STATUSES = ("verified", "unverified")
CR_STATES = ("open", "merged", "closed")
CR_TIERS = ("in-review", "on-main")
INPUT_KINDS = ("transcript", "ticket", "link", "notes", "document")
FINDING_KINDS = ("doc-drift", "stale-reference", "dead-config", "missing-doc", "naming-ux", "bug")
FINDING_STATUSES = ("collected", "reverified", "drafted", "approved", "filed",
                    "dropped", "merged", "rejected", "needs-review")

SHA_RE = re.compile(r"^[0-9a-f]{40}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
SLUG_RE = re.compile(r"^[a-z0-9][a-z0-9_-]*$")
DATE_RE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
URL_RE = re.compile(r"^[a-z][a-z0-9+.-]*://")
FINDING_ID_RE = re.compile(r"^UF-\d+$")


class ConfigError(Exception):
    """A configuration file failed validation.

    `problems` holds (key, message) pairs; str() renders `file:key: message` lines.
    """

    def __init__(self, path: Path | str, message: str, problems: list[tuple[str, str]] | None = None):
        self.path = Path(path)
        self.message = message
        self.problems = problems or [("", message)]
        super().__init__(str(self))

    def lines(self) -> list[str]:
        return [f"{self.path.name}:{key}: {msg}" if key else f"{self.path.name}: {msg}"
                for key, msg in self.problems]

    def __str__(self) -> str:
        return "\n".join(self.lines())


# --- entities -------------------------------------------------------------


@dataclass
class Persona:
    id: str
    title: str
    start: str


@dataclass
class Section:
    id: str
    title: str
    voice: str


@dataclass
class Cluster:
    name: str
    kubeconfig: str
    context: str | None = None
    disposable: bool = False


@dataclass
class Guide:
    root: Path
    schema: int
    title: str
    format: str
    personas: list[Persona]
    sections: list[Section]
    rigor: str = "standard"
    internal_enabled: bool = False
    confidential_terms: list[str] = field(default_factory=list)
    confidential_patterns: list[str] = field(default_factory=list)
    clusters: dict[str, Cluster] = field(default_factory=dict)
    extra_queries: list[str] = field(default_factory=list)
    drift_schedule: str = "off"
    upstream_target: str = "github"
    # The raw guide.yaml `branding` block (None without one, or when it is not a mapping).
    # load_guide does not check it: the checks live in branding.check_guide (run by
    # validate and playbook.py), so `get` keeps working for an invalid block and the
    # build reaches the step that reports it (FR-010).
    branding: dict | None = None

    @property
    def slug(self) -> str:
        return slugify(self.title)


@dataclass
class Source:
    id: str
    repo: str
    commit: str
    ref: str | None = None
    status: str = "verified"
    attributes: dict[str, str] = field(default_factory=dict)

    @property
    def web_url(self) -> str:
        """Browsable URL of the repository (GitHub shorthand expanded, .git stripped)."""
        return web_url(self.repo)

    @property
    def github_slug(self) -> str | None:
        """`owner/name` when the repository is on GitHub, else None."""
        m = re.match(r"^(?:https?://github\.com/|git@github\.com:)?([\w.-]+/[\w.-]+?)(?:\.git)?/?$", self.repo)
        if m and ("github.com" in self.repo or not URL_RE.match(self.repo)) and not self.repo.startswith("/"):
            return m.group(1)
        return None


@dataclass
class ChangeRequest:
    source: str
    number: int
    head: str
    state: str
    tier: str
    anchor: str


@dataclass
class Baseline:
    root: Path
    captured: str
    sources: list[Source]
    change_requests: list[ChangeRequest] = field(default_factory=list)

    def source(self, source_id: str) -> Source | None:
        return next((s for s in self.sources if s.id == source_id), None)

    def change_request(self, number: int) -> ChangeRequest | None:
        return next((c for c in self.change_requests if c.number == number), None)


@dataclass
class Input:
    id: str
    kind: str
    location: str
    role: str = ""
    sha256: str = ""
    confidential: bool = False
    registered: str = ""

    @property
    def is_url(self) -> bool:
        return bool(URL_RE.match(self.location))

    def path(self, root: Path) -> Path | None:
        """Filesystem path of the input (relative locations resolve against root)."""
        if self.is_url:
            return None
        p = Path(self.location).expanduser()
        return p if p.is_absolute() else (root / p)


@dataclass
class Inputs:
    root: Path
    inputs: list[Input] = field(default_factory=list)

    def get(self, input_id: str) -> Input | None:
        return next((i for i in self.inputs if i.id == input_id), None)


@dataclass
class Adapter:
    format: str
    content_root: str = "content"
    internal_root: str = "internal"
    page_glob: str = "**/*"
    comment: str = "line"
    requires: dict[str, str] = field(default_factory=dict)
    admonitions: dict[str, str] = field(default_factory=dict)
    experimental: dict[str, str] = field(default_factory=dict)
    attributes: str = ""
    preview_port: int = 8000
    image_dir: str = ""
    branding: list[str] = field(default_factory=list)   # supported branding keys
    extra: dict[str, Any] = field(default_factory=dict)


# --- helpers ----------------------------------------------------------------


def slugify(text: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-") or "guide"


def web_url(repo: str) -> str:
    if repo.startswith("git@github.com:"):
        repo = "https://github.com/" + repo.split(":", 1)[1]
    elif not URL_RE.match(repo) and re.match(r"^[\w.-]+/[\w.-]+$", repo):
        repo = "https://github.com/" + repo
    return re.sub(r"\.git/?$", "", repo.rstrip("/"))


def _read_yaml(path: Path) -> Any:
    if not path.is_file():
        raise ConfigError(path, "file not found")
    try:
        return yaml.safe_load(path.read_text()) or {}
    except yaml.YAMLError as exc:
        raise ConfigError(path, f"invalid YAML: {exc}") from exc


def _date(value: Any) -> str | None:
    if isinstance(value, (_dt.date, _dt.datetime)):
        return value.strftime("%Y-%m-%d")
    if isinstance(value, str) and DATE_RE.match(value):
        return value
    return None


class _Checker:
    """Collects (key, message) problems while reading one file."""

    def __init__(self, path: Path):
        self.path = path
        self.problems: list[tuple[str, str]] = []

    def add(self, key: str, message: str) -> None:
        self.problems.append((key, message))

    def mapping(self, value: Any, key: str, allowed: set[str], required: set[str] = frozenset()) -> dict:
        if value is None:
            value = {}
        if not isinstance(value, dict):
            self.add(key, "must be a mapping")
            return {}
        for k in value:
            if k not in allowed:
                self.add(f"{key}.{k}" if key else str(k), "unknown key")
        for k in sorted(required):
            if k not in value or value[k] in (None, ""):
                self.add(f"{key}.{k}" if key else k, "required")
        return value

    def str_list(self, value: Any, key: str) -> list[str]:
        if value is None:
            return []
        if not isinstance(value, list) or not all(isinstance(v, str) for v in value):
            self.add(key, "must be a list of strings")
            return []
        return value

    def enum(self, value: Any, key: str, choices: tuple[str, ...], default: str | None = None) -> str:
        if value is None and default is not None:
            return default
        if value not in choices:
            self.add(key, f"must be one of {', '.join(choices)} (got {value!r})")
            return default or ""
        return value

    def boolean(self, value: Any, key: str, default: bool = False) -> bool:
        if value is None:
            return default
        if not isinstance(value, bool):
            self.add(key, "must be true or false")
            return default
        return value

    def slug(self, value: Any, key: str) -> str:
        if not isinstance(value, str) or not SLUG_RE.match(value):
            self.add(key, f"must be a lowercase slug (got {value!r})")
            return str(value)
        return value

    def raise_if_any(self) -> None:
        if self.problems:
            raise ConfigError(self.path, self.problems[0][1], self.problems)


# --- loaders ----------------------------------------------------------------

GUIDE_KEYS = {"schema", "title", "format", "personas", "sections", "rigor", "internal",
              "clusters", "validate", "drift", "upstream", "branding"}


def load_adapter(root: Path, required: bool = True) -> Adapter | None:
    """Read tools/guide/adapter.yaml (the vendored adapter descriptor)."""
    path = Path(root) / "tools" / "guide" / "adapter.yaml"
    if not path.is_file():
        if required:
            raise ConfigError(path, "adapter descriptor not found (is the toolkit installed in tools/guide/?)")
        return None
    data = _read_yaml(path)
    known = {f for f in Adapter.__dataclass_fields__ if f != "extra"}
    kwargs = {k: v for k, v in data.items() if k in known}
    extra = {k: v for k, v in data.items() if k not in known}
    if "format" not in kwargs:
        raise ConfigError(path, "format is required")
    if not isinstance(kwargs.get("branding", []), list):
        raise ConfigError(path, "branding must be a list of branding keys")
    return Adapter(**kwargs, extra=extra)


def load_guide(root: Path) -> Guide:
    root = Path(root)
    path = root / "guide.yaml"
    c = _Checker(path)
    data = c.mapping(_read_yaml(path), "", GUIDE_KEYS, {"schema", "title", "format", "personas"})

    schema = data.get("schema")
    if schema is not None and schema != 1:
        c.add("schema", f"unsupported schema version {schema!r} (expected 1)")
    title = data.get("title")
    if title is not None and not isinstance(title, str):
        c.add("title", "must be a string")

    fmt = data.get("format")
    if fmt is not None and fmt not in FORMATS:
        c.add("format", f"no adapter for format {fmt!r} (available: {', '.join(FORMATS)})")
    adapter_path = root / "tools" / "guide" / "adapter.yaml"
    if fmt in FORMATS and adapter_path.is_file():
        vendored = (yaml.safe_load(adapter_path.read_text()) or {}).get("format")
        if vendored != fmt:
            c.add("format", f"{fmt!r} does not match the vendored adapter {vendored!r} in tools/guide/adapter.yaml")

    personas = []
    raw = data.get("personas")
    if raw is not None and (not isinstance(raw, list) or not raw):
        c.add("personas", "must be a non-empty list")
        raw = []
    for i, p in enumerate(raw or []):
        key = f"personas[{i}]"
        p = c.mapping(p, key, {"id", "title", "start"}, {"id", "title", "start"})
        personas.append(Persona(c.slug(p.get("id"), f"{key}.id"), str(p.get("title", "")), str(p.get("start", ""))))
    _unique(c, "personas", [p.id for p in personas])

    sections = []
    raw = data.get("sections") or []
    if not isinstance(raw, list):
        c.add("sections", "must be a list")
        raw = []
    for i, s in enumerate(raw):
        key = f"sections[{i}]"
        s = c.mapping(s, key, {"id", "title", "voice"}, {"id", "title", "voice"})
        sections.append(Section(c.slug(s.get("id"), f"{key}.id"), str(s.get("title", "")), str(s.get("voice", ""))))
    _unique(c, "sections", [s.id for s in sections])

    rigor = c.enum(data.get("rigor"), "rigor", RIGORS, "standard")

    internal = c.mapping(data.get("internal"), "internal", {"enabled", "confidential_terms", "confidential_patterns"})
    terms = c.str_list(internal.get("confidential_terms"), "internal.confidential_terms")
    patterns = c.str_list(internal.get("confidential_patterns"), "internal.confidential_patterns")
    for i, pat in enumerate(patterns):
        try:
            re.compile(pat)
        except re.error as exc:
            c.add(f"internal.confidential_patterns[{i}]", f"invalid regex: {exc}")

    clusters = {}
    raw = c.mapping(data.get("clusters"), "clusters", set(data.get("clusters") or {}))
    for name, cl in raw.items():
        key = f"clusters.{name}"
        cl = c.mapping(cl, key, {"kubeconfig", "context", "disposable"}, {"kubeconfig"})
        clusters[name] = Cluster(name, str(cl.get("kubeconfig", "")), cl.get("context"),
                                 c.boolean(cl.get("disposable"), f"{key}.disposable"))

    validate = c.mapping(data.get("validate"), "validate", {"extra_queries"})
    extra = c.str_list(validate.get("extra_queries"), "validate.extra_queries")

    drift = c.mapping(data.get("drift"), "drift", {"schedule"})
    schedule = drift.get("schedule", "off")
    if schedule is False or schedule is None:
        schedule = "off"
    if not isinstance(schedule, str) or (schedule != "off" and len(schedule.split()) != 5):
        c.add("drift.schedule", f"must be 'off' or a five-field cron expression (got {schedule!r})")

    upstream = c.mapping(data.get("upstream"), "upstream", {"target"})
    target = c.enum(upstream.get("target"), "upstream.target", ("github",), "github")

    brand = data.get("branding")
    if not isinstance(brand, dict):
        brand = None   # reported by branding.check_guide, not here (see Guide.branding)

    c.raise_if_any()
    return Guide(root=root, schema=int(schema), title=title, format=fmt, personas=personas,
                 sections=sections, rigor=rigor, internal_enabled=c.boolean(internal.get("enabled"), "internal.enabled"),
                 confidential_terms=terms, confidential_patterns=patterns, clusters=clusters,
                 extra_queries=extra, drift_schedule=schedule, upstream_target=target, branding=brand)


def load_baseline(root: Path) -> Baseline:
    root = Path(root)
    path = root / "baseline.yaml"
    c = _Checker(path)
    data = c.mapping(_read_yaml(path), "", {"captured", "sources", "change_requests"}, {"captured", "sources"})

    captured = _date(data.get("captured"))
    if data.get("captured") is not None and captured is None:
        c.add("captured", "must be a date (YYYY-MM-DD)")

    sources = []
    raw = data.get("sources")
    if raw is not None and (not isinstance(raw, list) or not raw):
        c.add("sources", "must be a non-empty list")
        raw = []
    for i, s in enumerate(raw or []):
        key = f"sources[{i}]"
        s = c.mapping(s, key, {"id", "repo", "ref", "commit", "status", "attributes"}, {"id", "repo", "commit"})
        repo = s.get("repo")
        if isinstance(repo, str) and repo.startswith("-"):
            c.add(f"{key}.repo", "must not start with '-' (it would be read as a git option)")
        commit = s.get("commit")
        if commit is not None and (not isinstance(commit, str) or not SHA_RE.match(commit)):
            c.add(f"{key}.commit", f"must be a full 40-character commit SHA (got {commit!r})")
        attrs = s.get("attributes") or {}
        if not isinstance(attrs, dict):
            c.add(f"{key}.attributes", "must be a mapping")
            attrs = {}
        for name, value in attrs.items():
            if isinstance(value, (dict, list)):
                c.add(f"{key}.attributes.{name}", "must be a scalar value")
        sources.append(Source(
            id=c.slug(s.get("id"), f"{key}.id"), repo=str(s.get("repo", "")), commit=str(commit or ""),
            ref=None if s.get("ref") is None else str(s.get("ref")),
            status=c.enum(s.get("status"), f"{key}.status", SOURCE_STATUSES, "verified"),
            attributes={str(k): _scalar(v) for k, v in attrs.items()},
        ))
    _unique(c, "sources", [s.id for s in sources])
    ids = {s.id for s in sources}

    crs = []
    raw = data.get("change_requests") or []
    if not isinstance(raw, list):
        c.add("change_requests", "must be a list")
        raw = []
    for i, cr in enumerate(raw):
        key = f"change_requests[{i}]"
        cr = c.mapping(cr, key, {"source", "number", "head", "state", "tier", "anchor"},
                       {"source", "number", "head", "state", "tier", "anchor"})
        if cr.get("source") is not None and cr.get("source") not in ids:
            c.add(f"{key}.source", f"unknown source {cr.get('source')!r}")
        number = cr.get("number")
        if number is not None and (not isinstance(number, int) or isinstance(number, bool)):
            c.add(f"{key}.number", "must be an integer")
        head = cr.get("head")
        if head is not None and (not isinstance(head, str) or not SHA_RE.match(head)):
            c.add(f"{key}.head", "must be a full 40-character commit SHA")
        crs.append(ChangeRequest(
            source=str(cr.get("source", "")), number=number if isinstance(number, int) else 0,
            head=str(head or ""), state=c.enum(cr.get("state"), f"{key}.state", CR_STATES),
            tier=c.enum(cr.get("tier"), f"{key}.tier", CR_TIERS), anchor=str(cr.get("anchor", "")),
        ))

    c.raise_if_any()
    return Baseline(root=root, captured=captured, sources=sources, change_requests=crs)


def load_inputs(root: Path) -> Inputs:
    root = Path(root)
    path = root / "inputs.yaml"
    if not path.is_file():
        return Inputs(root=root)
    c = _Checker(path)
    data = c.mapping(_read_yaml(path), "", {"inputs"})
    items = []
    raw = data.get("inputs") or []
    if not isinstance(raw, list):
        c.add("inputs", "must be a list")
        raw = []
    project = root.resolve()
    for i, it in enumerate(raw):
        key = f"inputs[{i}]"
        it = c.mapping(it, key, {"id", "kind", "location", "role", "sha256", "confidential", "registered"},
                       {"id", "kind", "location"})
        sha = it.get("sha256") or ""
        if sha and (not isinstance(sha, str) or not SHA256_RE.match(sha)):
            c.add(f"{key}.sha256", "must be a 64-character hex SHA-256 or empty")
        registered = it.get("registered")
        if registered is not None and _date(registered) is None:
            c.add(f"{key}.registered", "must be a date (YYYY-MM-DD)")
        item = Input(
            id=c.slug(it.get("id"), f"{key}.id"),
            kind=c.enum(it.get("kind"), f"{key}.kind", INPUT_KINDS),
            location=str(it.get("location", "")), role=str(it.get("role") or ""), sha256=sha,
            confidential=c.boolean(it.get("confidential"), f"{key}.confidential"),
            registered=_date(registered) or "",
        )
        p = item.path(root)
        if p is not None and item.location:
            resolved = p.resolve()
            if resolved == project or project in resolved.parents:
                c.add(f"{key}.location", "lies inside the guide project; raw inputs must stay where they live")
        items.append(item)
    _unique(c, "inputs", [i.id for i in items])
    c.raise_if_any()
    return Inputs(root=root, inputs=items)


def check_finding(finding: dict, sources: set[str] | None = None) -> list[str]:
    """Problems with one upstream finding entry (empty list when valid)."""
    c = _Checker(Path("findings/upstream.yaml"))
    allowed = {"id", "source", "kind", "locations", "summary", "evidence", "status",
               "reverified_commit", "filed", "merged_into", "note", "proposal"}
    f = c.mapping(finding, "", allowed, {"id", "source", "kind", "locations", "summary", "evidence", "status"})
    if f.get("id") is not None and not FINDING_ID_RE.match(str(f.get("id"))):
        c.add("id", "must look like UF-<n>")
    if sources is not None and f.get("source") is not None and f.get("source") not in sources:
        c.add("source", f"unknown source {f.get('source')!r}")
    if f.get("kind") is not None:
        c.enum(f.get("kind"), "kind", FINDING_KINDS)
    if f.get("status") is not None:
        c.enum(f.get("status"), "status", FINDING_STATUSES)
    locs = f.get("locations")
    if locs is not None:
        if not isinstance(locs, list) or not locs:
            c.add("locations", "must be a non-empty list")
        else:
            for i, loc in enumerate(locs):
                loc = c.mapping(loc, f"locations[{i}]", {"ref", "quote"}, {"ref"})
                ref = loc.get("ref")
                if ref is not None and not re.match(r"^[^:\s]+(:\d+(-\d+)?)?$", str(ref)):
                    c.add(f"locations[{i}].ref", "must be <path>[:<line>[-<line>]]")
    return [f"{k}: {m}" if k else m for k, m in c.problems]


def load_findings(root: Path) -> list[dict]:
    path = Path(root) / "findings" / "upstream.yaml"
    if not path.is_file():
        return []
    data = _read_yaml(path)
    return list(data.get("findings") or []) if isinstance(data, dict) else []


def pages(root: Path, adapter: Adapter, view: str = "all") -> list[Path]:
    """Content pages of a guide project for a view: shareable, internal, or all."""
    root = Path(root)
    content = root / adapter.content_root
    internal = root / adapter.internal_root
    found: set[Path] = set()
    if view in ("shareable", "all") and content.is_dir():
        found.update(p for p in content.glob(adapter.page_glob) if p.is_file())
    if view in ("internal", "all") and internal.is_dir():
        found.update(p for p in internal.glob(adapter.page_glob) if p.is_file())
    if view == "shareable":
        found = {p for p in found if not is_internal(p, root, adapter)}
    elif view == "internal":
        found = {p for p in found if is_internal(p, root, adapter)}
    return sorted(found)


def is_internal(page: Path, root: Path, adapter: Adapter) -> bool:
    internal = (Path(root) / adapter.internal_root).resolve()
    resolved = Path(page).resolve()
    return resolved == internal or internal in resolved.parents


def _scalar(value: Any) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (_dt.date, _dt.datetime)):
        return value.strftime("%Y-%m-%d")
    return "" if value is None else str(value)


def _unique(c: _Checker, key: str, ids: list[str]) -> None:
    seen = set()
    for i in ids:
        if i in seen:
            c.add(key, f"duplicate id {i!r}")
        seen.add(i)


# --- CLI --------------------------------------------------------------------


def validate(root: Path) -> tuple[list[str], list[str]]:
    """Problem and warning lines of guide.yaml, baseline.yaml, inputs.yaml, and the branding."""
    problems: list[str] = []
    for loader in (load_guide, load_baseline, load_inputs):
        try:
            loader(root)
        except ConfigError as err:
            problems.extend(err.lines())
    brand_problems, warnings = branding.check_guide(root)
    problems.extend(line for line in brand_problems if line not in problems)
    return problems, warnings


GET_KEYS = {
    "title": lambda g: g.title,
    "slug": lambda g: g.slug,
    "format": lambda g: g.format,
    "rigor": lambda g: g.rigor,
    "internal.enabled": lambda g: "true" if g.internal_enabled else "false",
    "drift.schedule": lambda g: g.drift_schedule,
}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Validate guide.yaml, baseline.yaml, and inputs.yaml")
    sub = parser.add_subparsers(dest="cmd", required=True)
    v = sub.add_parser("validate")
    v.add_argument("--root", type=Path, default=Path("."))
    g = sub.add_parser("get", help="print one guide.yaml value (for Makefiles)")
    g.add_argument("key", choices=sorted(GET_KEYS) + ["baseline.captured"])
    g.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args(argv)
    if args.cmd == "get":
        try:
            if args.key == "baseline.captured":
                print(load_baseline(args.root).captured)
            else:
                print(GET_KEYS[args.key](load_guide(args.root)))
        except ConfigError as err:
            print(err, file=sys.stderr)
            return 1
        return 0
    problems, warnings = validate(args.root)
    for line in problems + warnings:
        print(line)
    if problems:
        return 1
    print("guide configuration OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
