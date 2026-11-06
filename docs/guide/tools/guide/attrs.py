#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Baseline values for pages: the only way versions and commits reach the guide (FR-018).

Per source <id>: <id>-repo, <id>-commit, <id>-commit-short, <id>-blob, <id>-tree,
and <id>-<attribute> for every entry under `attributes`; plus baseline-date and
baseline-sources (plain text such as "mini 7ca3fb3, saw c84103d").

Usage:
  uv run tools/guide/attrs.py export --format antora [--root DIR]
      prints shell-quoted `--attribute name=value` arguments for the Antora CLI
  uv run tools/guide/attrs.py regions [--write] [--root DIR] DIR...
      generated regions in Markdown pages (gfm adapter):
        <!-- guide:attr NAME -->VALUE<!-- /guide:attr -->
      without --write, lists stale regions and exits 1 when any is stale;
      with --write, rewrites them in place. An unknown NAME always exits 1.
      A branding problem in guide.yaml (tools/guide/branding.py; the gfm adapter
      supports no branding key) exits 1 before any region is read or written, which
      stops the gfm `site` targets that run this first.
"""
from __future__ import annotations

import argparse
import re
import shlex
import sys
from dataclasses import dataclass
from pathlib import Path

import branding
import guidecfg


def values(baseline: guidecfg.Baseline) -> dict[str, str]:
    """All baseline values by attribute name, in a stable order."""
    out: dict[str, str] = {}
    for src in baseline.sources:
        url = src.web_url
        out[f"{src.id}-repo"] = url
        out[f"{src.id}-commit"] = src.commit
        out[f"{src.id}-commit-short"] = src.commit[:7]
        out[f"{src.id}-blob"] = f"{url}/blob/{src.commit}"
        out[f"{src.id}-tree"] = f"{url}/tree/{src.commit}"
        for name, value in src.attributes.items():
            out[f"{src.id}-{name}"] = value
    out["baseline-date"] = baseline.captured
    out["baseline-sources"] = ", ".join(f"{s.id} {s.commit[:7]}" for s in baseline.sources)
    return out


def export(baseline: guidecfg.Baseline, fmt: str) -> list[str]:
    """Command-line tokens that pass the baseline values to the format's build tool."""
    if fmt != "antora":
        raise ValueError(f"export supports only the antora format (got {fmt!r})")
    tokens: list[str] = []
    for name, value in values(baseline).items():
        tokens += ["--attribute", f"{name}={value}"]
    return tokens


REGION_RE = re.compile(r"<!--\s*guide:attr\s+([^\s>]+)\s*-->(.*?)<!--\s*/guide:attr\s*-->")
REGION_DIRS = ("docs", "internal")


class RegionError(ValueError):
    """A generated region names an attribute the baseline does not define."""


@dataclass
class Change:
    path: Path
    line: int
    name: str
    old: str
    new: str

    def __str__(self) -> str:
        return f"{self.path}:{self.line}: {self.name}: {self.old!r} -> {self.new!r}"


def regions(root: Path, baseline: guidecfg.Baseline, write: bool,
            dirs: list[str] | None = None) -> list[Change]:
    """Stale generated regions in the Markdown pages under dirs; rewritten when write is true.

    Raises RegionError (and writes nothing) when a region names an unknown attribute.
    Idempotent: a second call finds nothing to change.
    """
    root = Path(root)
    vals = values(baseline)
    files: list[Path] = []
    for d in dirs or REGION_DIRS:
        base = Path(d) if Path(d).is_absolute() else root / d
        if base.is_file():
            files.append(base)
        elif base.is_dir():
            files.extend(sorted(p for p in base.rglob("*.md") if "node_modules" not in p.parts))
    changes: list[Change] = []
    updated: dict[Path, str] = {}
    unknown: list[str] = []
    for path in files:
        text = path.read_text()
        for m in REGION_RE.finditer(text):
            name, old = m.group(1), m.group(2)
            line = text.count("\n", 0, m.start()) + 1
            if name not in vals:
                unknown.append(f"{path}:{line}: unknown baseline attribute {name!r}")
                continue
            if old != vals[name]:
                changes.append(Change(path, line, name, old, vals[name]))
        if write:
            new = REGION_RE.sub(lambda m: (f"<!-- guide:attr {m.group(1)} -->{vals[m.group(1)]}<!-- /guide:attr -->"
                                           if m.group(1) in vals else m.group(0)), text)
            if new != text:
                updated[path] = new
    if unknown:
        raise RegionError("\n".join(unknown))
    for path, text in updated.items():
        path.write_text(text)
    return changes


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Baseline values for guide pages")
    sub = parser.add_subparsers(dest="cmd", required=True)
    e = sub.add_parser("export")
    e.add_argument("--format", required=True, choices=["antora"])
    e.add_argument("--root", type=Path, default=Path("."))
    r = sub.add_parser("regions")
    r.add_argument("--write", action="store_true")
    r.add_argument("--root", type=Path, default=Path("."))
    r.add_argument("dirs", nargs="*")
    args = parser.parse_args(argv)
    if args.cmd == "regions":
        problems, _ = branding.check_guide(args.root)
        if problems:
            print("\n".join(problems), file=sys.stderr)
            return 1
    try:
        baseline = guidecfg.load_baseline(args.root)
    except guidecfg.ConfigError as err:
        print(err, file=sys.stderr)
        return 1
    if args.cmd == "export":
        print(shlex.join(export(baseline, args.format)))
        return 0
    try:
        changes = regions(args.root, baseline, args.write, args.dirs or None)
    except RegionError as err:
        print(err, file=sys.stderr)
        return 1
    if args.write:
        print(f"regions: {len(changes)} updated")
        return 0
    for c in changes:
        print(f"stale region {c}")
    if changes:
        print("regions: run `uv run tools/guide/attrs.py regions --write` (make site does it)")
        return 1
    print("regions: up to date")
    return 0


if __name__ == "__main__":
    sys.exit(main())
