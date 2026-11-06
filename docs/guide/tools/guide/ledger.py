#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Strict rigor: validate the claim ledger and every page's source block (FR-021).

Ledger checks: ID format (C-<AREA>-<nn>) and uniqueness, status values (CONFIRMED,
CONTRADICTED, OPEN), that every listed page exists, and that every source reference
resolves at the pinned commit. Page checks: every non-stub shareable page has a source
block at the top of the page (never inside a listing) whose references resolve.

Ledger location (override with --ledger):
  antora          <internal_root>/modules/internal/pages/claim-ledger.adoc  (|=== table)
  mkdocs, gfm     <internal_root>/claim-ledger.md                           (pipe table)
Columns: ID, Claim, Page, Source, Status. Page and Source take several values separated
by `;`; Page is `-` when a claim belongs to no single page. Pages are named by their path
without extension, relative to the page directory.

Usage: uv run tools/guide/ledger.py [--root DIR] [--ledger FILE]   (exit 1 on errors)
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

import guidecfg
import sources

ID_RE = re.compile(r"^C-[A-Z]+-\d{2,}$")
STATUSES = {"CONFIRMED", "CONTRADICTED", "OPEN"}
HEADER = ["ID", "Claim", "Page", "Source", "Status"]
STUB_RE = re.compile(r"^(// STUB|<!--\s*STUB)", re.M)


class LedgerError(ValueError):
    """The ledger file cannot be read as a table."""


def default_ledger(root: Path, adapter: guidecfg.Adapter) -> Path:
    internal = Path(root) / adapter.internal_root
    if adapter.format == "antora":
        return internal / "modules" / "internal" / "pages" / "claim-ledger.adoc"
    return internal / "claim-ledger.md"


def _rows(cells: list[str], path: Path) -> list[dict[str, str]]:
    if cells[:5] != HEADER:
        raise LedgerError(f"{path}: unexpected header {cells[:5]} (expected {HEADER})")
    body = cells[5:]
    if len(body) % 5:
        raise LedgerError(f"{path}: every row needs exactly five cells")
    return [dict(zip(HEADER, body[i:i + 5])) for i in range(0, len(body), 5)]


def parse_ledger(path: Path) -> list[dict[str, str]]:
    """Rows of the claim ledger (AsciiDoc |=== table or Markdown pipe table)."""
    path = Path(path)
    lines = path.read_text(errors="replace").splitlines()
    if path.suffix == ".adoc":
        try:
            start = lines.index("|===") + 1
            end = lines.index("|===", start)
        except ValueError as exc:
            raise LedgerError(f"{path}: table delimiters '|===' not found") from exc
        cells: list[str] = []
        for line in lines[start:end]:
            if line.startswith("|"):
                cells.extend(part.strip() for part in line[1:].split("|"))
            elif line.strip() and cells:
                cells[-1] = f"{cells[-1]} {line.strip()}"
        return _rows(cells, path)
    cells = []
    seen_header = False
    for line in lines:
        stripped = line.strip()
        if not stripped.startswith("|"):
            if seen_header and cells:
                break
            continue
        row = [c.strip() for c in stripped.strip("|").split("|")]
        if all(re.fullmatch(r":?-{3,}:?", c) for c in row if c):
            continue
        seen_header = True
        cells.extend(row)
    if not cells:
        raise LedgerError(f"{path}: no pipe table found")
    return _rows(cells, path)


def page_key(page: Path, root: Path, adapter: guidecfg.Adapter) -> str:
    """Page name used in the ledger: path without extension below the page directory."""
    page = Path(page)
    parts = page.parts
    if adapter.format == "antora" and "pages" in parts:
        idx = len(parts) - 1 - parts[::-1].index("pages")
        return Path(*parts[idx + 1:]).with_suffix("").as_posix()
    for base in (Path(root) / adapter.internal_root, Path(root) / adapter.content_root):
        try:
            return page.relative_to(base).with_suffix("").as_posix()
        except ValueError:
            continue
    return page.with_suffix("").name


def _split(value: str) -> list[str]:
    return [part.strip() for part in value.split(";") if part.strip()]


def check(root: Path, ledger_path: Path | None = None) -> tuple[list[str], dict[str, int]]:
    """Return (errors, stats) for the claim ledger and the shareable pages."""
    root = Path(root)
    adapter = guidecfg.load_adapter(root)
    baseline = guidecfg.load_baseline(root)
    inputs = guidecfg.load_inputs(root)
    cache = root / ".guide" / "cache"
    style = "line" if adapter.comment == "line" else "html"
    ledger_path = Path(ledger_path) if ledger_path else default_ledger(root, adapter)
    errors: list[str] = []
    stats = {"claims": 0, "pages": 0, "sources": 0}

    def check_ref(label: str, text: str) -> None:
        try:
            ref = sources.parse_ref(text)
        except sources.BlockError as err:
            errors.append(f"{label}: {err}")
            return
        stats["sources"] += 1
        res = sources.resolve(ref, baseline, cache, inputs)
        if not res.ok:
            errors.append(f"{label}: {text}: {res.message}")

    all_pages = guidecfg.pages(root, adapter, "all")
    known = {page_key(p, root, adapter) for p in all_pages}

    if not ledger_path.is_file():
        errors.append(f"ledger not found: {ledger_path}")
        rows = []
    else:
        try:
            rows = parse_ledger(ledger_path)
        except LedgerError as err:
            errors.append(str(err))
            rows = []
    seen: set[str] = set()
    for row in rows:
        cid = row["ID"]
        stats["claims"] += 1
        if not ID_RE.match(cid):
            errors.append(f"{cid}: bad ID format (expected C-<AREA>-<nn>)")
        if cid in seen:
            errors.append(f"{cid}: duplicate ID")
        seen.add(cid)
        if row["Status"] not in STATUSES:
            errors.append(f"{cid}: bad status {row['Status']!r} (expected one of {', '.join(sorted(STATUSES))})")
        if row["Page"] != "-":
            for page in _split(row["Page"]):
                if page not in known:
                    errors.append(f"{cid}: page {page} does not exist")
        refs = _split(row["Source"])
        if not refs:
            errors.append(f"{cid}: no source")
        for ref in refs:
            check_ref(cid, ref)

    for page in guidecfg.pages(root, adapter, "shareable"):
        text = page.read_text(errors="replace")
        if STUB_RE.search(text):
            continue
        stats["pages"] += 1
        label = page_key(page, root, adapter)
        for problem in sources.check_text(text, style, label):
            errors.append(f"{label}: {problem.message}")
        block = sources.find_block(text, style)
        for ref in (block.refs if block else []):
            stats["sources"] += 1
            res = sources.resolve(ref, baseline, cache, inputs)
            if not res.ok:
                errors.append(f"{label}: {ref.raw}: {res.message}")
    return errors, stats


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Validate the claim ledger and the pages' source blocks")
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--ledger", type=Path)
    args = parser.parse_args(argv)
    try:
        errors, stats = check(args.root, args.ledger)
    except guidecfg.ConfigError as err:
        print(f"FAIL ledger: {err}")
        return 1
    for error in errors:
        print(f"FAIL ledger: {error}")
    print(f"ledger: {stats['claims']} claims, {stats['pages']} pages checked, "
          f"{stats['sources']} sources checked, {len(errors)} errors")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
