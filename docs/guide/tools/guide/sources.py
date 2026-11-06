#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Parse source reference blocks ("source blocks") and resolve them at the pinned commits.

A source block sits directly after the page title (contracts/page-conventions.md, section 1):

  AsciiDoc (style "line"):  // Sources:            Markdown (style "html"):  <!-- Sources:
                            // mini:README.md:1-3                            mini:README.md:1-3
                                                                             -->
Grammar per entry: <source-id>:<path>[:<lines>] | url:<absolute-url> | input:<input-id>

Usage: uv run tools/guide/sources.py check [--root DIR] [--style line|html] PAGE...
Exits 1 and prints `page:line: message` for every problem.
"""
from __future__ import annotations

import argparse
import functools
import re
import subprocess
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Literal

import guidecfg

Style = Literal["line", "html"]

SOURCE_REF_RE = re.compile(r"^([a-z0-9][a-z0-9_-]*):([^:\s]+)(?::([0-9][0-9,-]*))?$")
RANGE_RE = re.compile(r"^(\d+)(?:-(\d+))?$")
URL_RE = re.compile(r"^[a-z][a-z0-9+.-]*://\S+$")
INPUT_RE = re.compile(r"^[a-z0-9][a-z0-9_-]*$")
ADOC_ATTR_RE = re.compile(r"^:[!\w][\w-]*!?:")
NO_SOURCES_ADOC = ":guide-no-sources:"
NO_SOURCES_MD = re.compile(r"^<!--\s*guide:\s*no-sources\s*-->$")


# Links between pages, shared by check.py (internal and broken links) and drift.py
# (inbound links of the impact set) so both read the same link syntax.
XREF_RE = re.compile(r"xref:([^\s\[]+)\[")
MD_LINK_RE = re.compile(r"\]\(\s*<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\s*\)|href=\"([^\"]+)\"")
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.I)


def link_targets(text: str) -> list[str]:
    """Targets of Markdown links, image links, and href attributes in text."""
    return [m.group(1) or m.group(2) for m in MD_LINK_RE.finditer(text)]


class BlockError(ValueError):
    """A source block entry does not follow the grammar."""


@dataclass(frozen=True)
class Ref:
    kind: str  # "source" | "url" | "input"
    source_id: str | None = None
    path: str | None = None
    ranges: tuple[tuple[int, int], ...] = ()
    url: str | None = None
    input_id: str | None = None
    line: int = 0
    raw: str = ""


@dataclass
class Block:
    start_line: int
    refs: list[Ref] = field(default_factory=list)
    errors: list[tuple[int, str]] = field(default_factory=list)
    no_sources: bool = False


@dataclass
class Resolution:
    ok: bool
    message: str = ""


@dataclass
class Problem:
    page: str
    line: int
    message: str

    def __str__(self) -> str:
        return f"{self.page}:{self.line}: {self.message}"


def parse_ref(text: str, line: int = 0) -> Ref:
    """Parse one source block entry; raise BlockError when it does not follow the grammar."""
    raw = text.strip()
    if raw.startswith("url:"):
        url = raw[4:]
        if not URL_RE.match(url):
            raise BlockError(f"bad url reference {raw!r} (expected url:<absolute-url>)")
        return Ref(kind="url", url=url, line=line, raw=raw)
    if raw.startswith("input:"):
        input_id = raw[6:]
        if not INPUT_RE.match(input_id):
            raise BlockError(f"bad input reference {raw!r} (expected input:<input-id>)")
        return Ref(kind="input", input_id=input_id, line=line, raw=raw)
    m = SOURCE_REF_RE.match(raw)
    if not m:
        raise BlockError(f"bad source reference {raw!r} (expected <source-id>:<path>[:<lines>])")
    ranges = []
    if m.group(3) is not None:
        for part in m.group(3).split(","):
            rm = RANGE_RE.match(part)
            if not rm:
                raise BlockError(f"bad line range {part!r} in {raw!r}")
            first = int(rm.group(1))
            last = int(rm.group(2) or first)
            if first < 1 or last < first:
                raise BlockError(f"bad line range {part!r} in {raw!r}")
            ranges.append((first, last))
    return Ref(kind="source", source_id=m.group(1), path=m.group(2), ranges=tuple(ranges), line=line, raw=raw)


def find_block(text: str, style: Style) -> Block | None:
    """Locate the source block in the page header; None when the page has none.

    The header is the title plus the lines up to the first body line. Attribute
    entries (AsciiDoc), comments, front matter (Markdown), and blank lines belong
    to the header, so a block inside a listing or after body text is never found.
    """
    lines = text.splitlines()
    i = 0
    n = len(lines)
    if style == "html" and lines and lines[0].strip() == "---":
        i = 1
        while i < n and lines[i].strip() != "---":
            i += 1
        i += 1
    while i < n and not lines[i].strip():
        i += 1
    title = r"^=\s" if style == "line" else r"^#\s"
    if i < n and re.match(title, lines[i]):
        i += 1
    no_sources = False
    block: Block | None = None
    while i < n:
        line = lines[i]
        stripped = line.strip()
        if style == "line":
            if stripped == NO_SOURCES_ADOC:
                no_sources = True
            elif stripped == "// Sources:":
                block = Block(start_line=i + 1)
                i += 1
                while i < n and lines[i].startswith("//") and not lines[i].startswith("////"):
                    entry = lines[i][2:].strip()
                    if entry:
                        _add(block, entry, i + 1)
                    i += 1
                continue
            elif not stripped or ADOC_ATTR_RE.match(stripped) or (
                    stripped.startswith("//") and not stripped.startswith("////")):
                pass
            else:
                break
        else:
            if NO_SOURCES_MD.match(stripped):
                no_sources = True
            elif stripped.startswith("<!--") and stripped[4:].strip().startswith("Sources:"):
                block = Block(start_line=i + 1)
                rest = stripped[4:].strip()[len("Sources:"):].strip()
                closed = False
                if rest:
                    closed = _html_entry(block, rest, i + 1)
                i += 1
                while not closed and i < n:
                    closed = _html_entry(block, lines[i].strip(), i + 1)
                    i += 1
                if not closed:
                    block.errors.append((block.start_line, "source block is not closed with -->"))
                continue
            elif stripped.startswith("<!--"):
                # another header comment; skip to its end
                while i < n and "-->" not in lines[i]:
                    i += 1
            elif not stripped:
                pass
            else:
                break
        i += 1
    if block is None and no_sources:
        return Block(start_line=0, no_sources=True)
    if block is not None:
        block.no_sources = no_sources
    return block


def _html_entry(block: Block, entry: str, line: int) -> bool:
    """Add one Markdown block line; return True when the line closes the block."""
    closed = entry.endswith("-->")
    if closed:
        entry = entry[:-3].strip()
    if entry:
        _add(block, entry, line)
    return closed


def _add(block: Block, entry: str, line: int) -> None:
    try:
        block.refs.append(parse_ref(entry, line))
    except BlockError as err:
        block.errors.append((line, str(err)))


def parse_block(text: str, style: Style) -> list[Ref]:
    """References in the page's source block (empty when there is none).

    Raises BlockError when an entry does not follow the grammar.
    """
    block = find_block(text, style)
    if block is None:
        return []
    if block.errors:
        line, msg = block.errors[0]
        raise BlockError(f"line {line}: {msg}")
    return block.refs


def check_text(text: str, style: Style, page: str = "") -> list[Problem]:
    """Structural problems of a page's source block (no resolution against sources)."""
    block = find_block(text, style)
    if block is None:
        return [Problem(page, 1, "missing source block (add one after the title, or mark the page as no-sources)")]
    problems = [Problem(page, line, msg) for line, msg in block.errors]
    if not block.refs and not block.no_sources and not block.errors:
        problems.append(Problem(page, block.start_line, "empty source block (allowed only on pages marked no-sources)"))
    return problems


@functools.lru_cache(maxsize=4096)
def _line_count(gitdir: str, commit: str, path: str) -> int | None:
    try:
        proc = subprocess.run(["git", "-C", gitdir, "show", f"{commit}:{path}"],
                              capture_output=True, timeout=120)
    except subprocess.TimeoutExpired:
        return None
    if proc.returncode != 0:
        return None
    data = proc.stdout
    if not data:
        return 0
    return data.count(b"\n") + (0 if data.endswith(b"\n") else 1)


def _gitdir(cache_dir: Path, source_id: str) -> Path | None:
    for candidate in (cache_dir / source_id, cache_dir / f"{source_id}.git"):
        if candidate.exists():
            return candidate
    return None


def resolve(ref: Ref, baseline: guidecfg.Baseline, cache_dir: Path,
            inputs: guidecfg.Inputs | None = None) -> Resolution:
    """Check that a reference points at something that exists at the pinned commit."""
    if ref.kind == "url":
        return Resolution(True)
    if ref.kind == "input":
        if inputs is None:
            return Resolution(True, "input register not checked")
        if inputs.get(ref.input_id) is None:
            return Resolution(False, f"unknown input {ref.input_id!r} (not in inputs.yaml)")
        return Resolution(True)
    source = baseline.source(ref.source_id)
    if source is None:
        return Resolution(False, f"unknown source {ref.source_id!r} (not in baseline.yaml)")
    if source.status == "unverified":
        return Resolution(True, f"source {source.id!r} is unverified; reference not checked")
    gitdir = _gitdir(Path(cache_dir), source.id)
    if gitdir is None:
        return Resolution(False, f"source {source.id!r} is not fetched (run make fetch-baseline)")
    count = _line_count(str(gitdir), source.commit, ref.path)
    if count is None:
        return Resolution(False, f"{ref.path} not found in {source.id} at {source.commit[:7]}")
    for first, last in ref.ranges:
        if last > count:
            return Resolution(False, f"lines {first}-{last} beyond the end of {ref.path} "
                                     f"({count} lines at {source.commit[:7]})")
    return Resolution(True)


def cited_files(pages: list[Path], style: Style) -> dict[str, set[str]]:
    """Source paths cited in the pages' source blocks, per source id."""
    cited: dict[str, set[str]] = {}
    for page in pages:
        block = find_block(Path(page).read_text(errors="replace"), style)
        for ref in (block.refs if block else []):
            if ref.kind == "source":
                cited.setdefault(ref.source_id, set()).add(ref.path)
    return cited


def check_pages(pages: list[Path], style: Style, root: Path, baseline: guidecfg.Baseline,
                inputs: guidecfg.Inputs | None = None) -> list[Problem]:
    """Structure and resolution problems for every page."""
    cache = Path(root) / ".guide" / "cache"
    problems: list[Problem] = []
    for page in pages:
        page = Path(page)
        try:
            label = page.resolve().relative_to(Path(root).resolve()).as_posix()
        except ValueError:
            label = str(page)
        text = page.read_text(errors="replace")
        problems.extend(check_text(text, style, label))
        block = find_block(text, style)
        for ref in (block.refs if block else []):
            res = resolve(ref, baseline, cache, inputs)
            if not res.ok:
                problems.append(Problem(label, ref.line, res.message))
    return problems


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Check source blocks against the pinned sources")
    sub = parser.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("check")
    c.add_argument("--root", type=Path, default=Path("."))
    c.add_argument("--style", choices=["line", "html"])
    c.add_argument("pages", nargs="*", type=Path)
    args = parser.parse_args(argv)

    root = args.root
    style = args.style
    pages = args.pages
    if style is None or not pages:
        adapter = guidecfg.load_adapter(root)
        style = style or ("line" if adapter.comment == "line" else "html")
        pages = pages or guidecfg.pages(root, adapter, "all")
    try:
        baseline = guidecfg.load_baseline(root)
        inputs = guidecfg.load_inputs(root)
    except guidecfg.ConfigError as err:
        print(err)
        return 1
    problems = check_pages(pages, style, root, baseline, inputs)
    for p in problems:
        print(p)
    if problems:
        return 1
    print(f"sources: {len(pages)} pages checked")
    return 0


if __name__ == "__main__":
    sys.exit(main())
