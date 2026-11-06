#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Adopt a guide that was not created by cc-guide (FR-028).

  migrate --map FILE   translate a foreign baseline.yaml into the cc-guide schema with an
                       adoption map (see templates/adopt-map.example.yaml); the original
                       is kept as baseline.foreign.yaml
  report               list the pages that still lack a source block

Usage: uv run tools/guide/adopt.py migrate --map FILE [--root DIR]
       uv run tools/guide/adopt.py report [--root DIR]
"""
from __future__ import annotations

import argparse
import datetime as _dt
import re
import sys
from pathlib import Path
from typing import Any

import yaml

import guidecfg
import sources

PATH_TOKEN_RE = re.compile(r"\.([A-Za-z0-9_-]+)|\[(\d+)\]")
_MISSING = object()


class AdoptError(Exception):
    """The foreign baseline could not be migrated."""


# --- value expressions ---------------------------------------------------------


def _walk(path: str, start: Any) -> Any:
    """Follow a path like `.a.b[0].c` from start; _MISSING when any step is absent."""
    if path == ".":
        return start
    pos = 0
    value = start
    while pos < len(path):
        m = PATH_TOKEN_RE.match(path, pos)
        if not m:
            raise AdoptError(f"bad path {path!r} in the adoption map")
        key, index = m.group(1), m.group(2)
        if key is not None:
            if not isinstance(value, dict) or key not in value:
                return _MISSING
            value = value[key]
        else:
            i = int(index)
            if not isinstance(value, list) or i >= len(value):
                return _MISSING
            value = value[i]
        pos = m.end()
    return value


def _lookup(expr: str, doc: Any, item: Any) -> Any:
    if expr.startswith("$."):
        return _walk(expr[1:], doc)
    return _walk(expr, item if item is not None else doc)


def resolve(expr: Any, doc: Any, item: Any = None) -> Any:
    """Evaluate one adoption-map value against the foreign document (None when missing)."""
    if isinstance(expr, dict):
        if "value" in expr:
            return expr["value"]
        if "path" in expr:
            found = _lookup(str(expr["path"]), doc, item)
            return expr.get("default") if found is _MISSING else found
        if "by" in expr:
            key = resolve(expr["by"], doc, item)
            table = expr.get("map") or {}
            for candidate in (key, str(key)):
                if candidate in table:
                    return table[candidate]
            if isinstance(key, str) and key.isdigit() and int(key) in table:
                return table[int(key)]
            return expr.get("default")
        raise AdoptError(f"unknown value form {expr!r} in the adoption map")
    if isinstance(expr, str) and (expr.startswith(".") or expr.startswith("$.")):
        found = _lookup(expr, doc, item)
        return None if found is _MISSING else found
    return expr


def _text(value: Any) -> Any:
    if isinstance(value, (_dt.date, _dt.datetime)):
        return value.strftime("%Y-%m-%d")
    return value


def _describe(expr: Any) -> str:
    if isinstance(expr, dict):
        return str(expr.get("path") or expr.get("by") or expr)
    return str(expr)


def build(doc: dict, spec: dict) -> dict:
    """The cc-guide baseline data for a foreign document and an adoption map."""
    problems: list[str] = []

    def required(expr: Any, where: str, item: Any = None) -> Any:
        value = resolve(expr, doc, item)
        if value is None or value == "":
            problems.append(f"{where}: {_describe(expr)} has no value in the foreign baseline")
        return _text(value)

    out: dict[str, Any] = {"captured": required(spec.get("captured"), "captured"), "sources": []}
    for i, s in enumerate(spec.get("sources") or []):
        where = f"sources[{i}]"
        entry: dict[str, Any] = {"id": required(s.get("id"), f"{where}.id"),
                                 "repo": required(s.get("repo"), f"{where}.repo")}
        if s.get("ref") is not None:
            ref = resolve(s["ref"], doc)
            if ref is not None:
                entry["ref"] = str(_text(ref))
        entry["commit"] = required(s.get("commit"), f"{where}.commit")
        entry["status"] = s.get("status", "verified")
        attrs = {}
        for name, expr in (s.get("attributes") or {}).items():
            value = resolve(expr, doc)
            if value is not None:
                attrs[str(name)] = _text(value)
        if attrs:
            entry["attributes"] = attrs
        out["sources"].append(entry)

    crs = []
    fields = ("source", "number", "head", "state", "tier", "anchor")
    for i, c in enumerate(spec.get("change_requests") or []):
        each = c.get("each")
        if each is not None:
            if not isinstance(each, str) or not each.endswith("[]"):
                problems.append(f"change_requests[{i}].each: must be a path ending in [] (got {each!r})")
                continue
            items = _lookup(each[:-2], doc, None)
            if items is _MISSING or items is None:
                items = []
            if not isinstance(items, list):
                problems.append(f"change_requests[{i}].each: {each} is not a list")
                continue
        else:
            items = [None]
        for j, item in enumerate(items):
            where = f"change_requests[{i}]" + (f" item {j}" if each else "")
            crs.append({f: required(c.get(f), f"{where}.{f}", item) for f in fields})
    if crs:
        out["change_requests"] = crs
    if problems:
        raise AdoptError("cannot migrate the baseline:\n  " + "\n  ".join(problems))
    return out


# --- operations ------------------------------------------------------------------


def migrate(root: Path, mapping: Path) -> guidecfg.Baseline:
    """Translate root/baseline.yaml with the adoption map; keep the original as baseline.foreign.yaml."""
    root = Path(root)
    target = root / "baseline.yaml"
    if not target.is_file():
        raise AdoptError(f"no baseline.yaml in {root} to migrate")
    foreign_text = target.read_text()
    doc = yaml.safe_load(foreign_text) or {}
    if isinstance(doc, dict) and isinstance(doc.get("sources"), list):
        raise AdoptError("baseline.yaml already uses the cc-guide schema; nothing to migrate")
    spec = yaml.safe_load(Path(mapping).read_text()) or {}
    data = build(doc, spec)

    foreign = root / "baseline.foreign.yaml"
    if foreign.exists() and foreign.read_text() != foreign_text:
        raise AdoptError("baseline.foreign.yaml exists and differs from baseline.yaml; move it away first")
    created = not foreign.exists()
    foreign.write_text(foreign_text)
    header = (f"# Baseline migrated by tools/guide/adopt.py with {Path(mapping).name}.\n"
              "# The original file is kept as baseline.foreign.yaml.\n")
    target.write_text(header + yaml.safe_dump(data, sort_keys=False, allow_unicode=True))
    try:
        return guidecfg.load_baseline(root)
    except guidecfg.ConfigError as err:
        target.write_text(foreign_text)
        if created:
            foreign.unlink()
        raise AdoptError(f"the migrated baseline is not valid:\n{err}") from err


def missing_blocks(root: Path) -> list[Path]:
    """Pages without a source block, or with an empty one that is not marked no-sources."""
    root = Path(root)
    adapter = guidecfg.load_adapter(root)
    style = "line" if adapter.comment == "line" else "html"
    missing = []
    for page in guidecfg.pages(root, adapter, "all"):
        block = sources.find_block(page.read_text(errors="replace"), style)
        if block is None or (not block.refs and not block.no_sources):
            missing.append(page)
    return sorted(missing)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Adopt a guide that was not created by cc-guide")
    sub = parser.add_subparsers(dest="cmd", required=True)
    m = sub.add_parser("migrate")
    m.add_argument("--map", required=True, type=Path)
    m.add_argument("--root", type=Path, default=Path("."))
    r = sub.add_parser("report")
    r.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args(argv)
    try:
        if args.cmd == "migrate":
            baseline = migrate(args.root, args.map)
            for s in baseline.sources:
                print(f"{s.id}: {s.commit} ({s.ref or '-'})")
            print(f"{len(baseline.change_requests)} change request(s); original kept as baseline.foreign.yaml")
        else:
            root = args.root
            missing = missing_blocks(root)
            for page in missing:
                try:
                    print(page.resolve().relative_to(Path(root).resolve()).as_posix())
                except ValueError:
                    print(page)
            print(f"{len(missing)} page(s) without a source block")
    except (AdoptError, guidecfg.ConfigError) as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
