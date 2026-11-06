#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml", "pypdf"]
# ///
"""Leak check for built shareable output: sites and PDFs.

Reports confidential terms (case-insensitive) and confidential patterns (regex)
from guide.yaml, links into the internal part, and internal directories in a
site. HTML is scanned as visible text plus alt, title, and aria-label values and
comment text; script and style elements are ignored, so vendor JavaScript cannot
trigger false positives. Search index JSON and plain text files are scanned as
they are. A link or path is internal when one of its path segments equals the
last component of the adapter's internal_root (default "internal"); absolute
URLs to other sites are not internal links.

Usage: uv run tools/guide/leakcheck.py [--root DIR] DIR_OR_PDF...
Exits 1 and prints `path:line: [kind] match` for every leak (PDF: line = page number).
"""
from __future__ import annotations

import argparse
import html
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import guidecfg

SCRIPT_STYLE_RE = re.compile(r"<(script|style)\b[^>]*>.*?</\1\s*>", re.S | re.I)
COMMENT_RE = re.compile(r"<!--(.*?)-->", re.S)
TAG_RE = re.compile(r"<[^>]*>", re.S)
TEXT_ATTR_RE = re.compile(r"\b(?:alt|title|aria-label)\s*=\s*(\"[^\"]*\"|'[^']*')", re.I)
LINK_ATTR_RE = re.compile(r"\b(?:href|src)\s*=\s*(\"[^\"]*\"|'[^']*')", re.I)
SCHEME_RE = re.compile(r"^[a-z][a-z0-9+.-]*:", re.I)
HTML_SUFFIXES = {".html", ".htm"}
TEXT_SUFFIXES = {".txt", ".md", ".adoc"}


@dataclass
class Leak:
    path: str
    line: int
    kind: str  # term | pattern | internal-link | internal-path
    match: str

    def __str__(self) -> str:
        what = {
            "term": "confidential term",
            "pattern": "confidential pattern match",
            "internal-link": "link into the internal part",
            "internal-path": "internal content in the shareable output",
        }.get(self.kind, self.kind)
        return f"{self.path}:{self.line}: [{self.kind}] {what}: {self.match}"


def _keep_lines(text: str) -> str:
    return "\n" * text.count("\n")


def _visible_text(raw: str) -> str:
    """HTML reduced to visible text, alt/title/aria-label values, and comment text; line breaks kept."""
    text = SCRIPT_STYLE_RE.sub(lambda m: _keep_lines(m.group(0)), raw)
    text = COMMENT_RE.sub(lambda m: " " + m.group(1) + " ", text)

    def tag(m: re.Match) -> str:
        values = [v[1:-1] for v in TEXT_ATTR_RE.findall(m.group(0))]
        return " " + " ".join(values) + " " + _keep_lines(m.group(0))

    return html.unescape(TAG_RE.sub(tag, text))


def _is_internal_target(target: str, marker: str) -> bool:
    target = target.strip()
    if not target or target.startswith("#") or target.startswith("//"):
        return False
    if SCHEME_RE.match(target) and not target.lower().startswith("file:"):
        return False
    path = re.sub(r"^file:/*", "/", target, flags=re.I).split("#", 1)[0].split("?", 1)[0]
    return marker in [seg for seg in path.split("/") if seg]


def _scan_text(text: str, label: str, guide: guidecfg.Guide, line_of=None) -> list[Leak]:
    leaks: list[Leak] = []
    terms = [re.compile(re.escape(t), re.I) for t in guide.confidential_terms]
    patterns = [re.compile(p) for p in guide.confidential_patterns]
    for n, line in enumerate(text.splitlines(), 1):
        where = line_of(n) if line_of else n
        for rx in terms:
            for m in rx.finditer(line):
                leaks.append(Leak(label, where, "term", m.group(0)))
        for rx in patterns:
            for m in rx.finditer(line):
                leaks.append(Leak(label, where, "pattern", m.group(0)))
    return leaks


def scan_html(path: Path, label: str, guide: guidecfg.Guide, internal_marker: str = "internal") -> list[Leak]:
    raw = Path(path).read_text(errors="replace")
    leaks = _scan_text(_visible_text(raw), label, guide)
    no_scripts = SCRIPT_STYLE_RE.sub(lambda m: _keep_lines(m.group(0)), raw)
    for m in LINK_ATTR_RE.finditer(no_scripts):
        target = html.unescape(m.group(1)[1:-1])
        line = no_scripts.count("\n", 0, m.start()) + 1
        if _is_internal_target(target, internal_marker):
            leaks.append(Leak(label, line, "internal-link", target))
        # A confidential term or ticket key can also hide in a link target (FR-019, FR-030e).
        leaks.extend(_scan_text(target, label, guide, line_of=lambda _n, line=line: line))
    return leaks


def scan_dir(path: Path, guide: guidecfg.Guide, internal_marker: str = "internal") -> list[Leak]:
    """Leaks in a built site directory."""
    base = Path(path)
    leaks: list[Leak] = []
    for dirpath, dirnames, filenames in os.walk(base):
        current = Path(dirpath)
        for d in sorted(dirnames):
            if d == internal_marker:
                leaks.append(Leak(str(current / d), 0, "internal-path", d))
        dirnames[:] = sorted(d for d in dirnames if d != internal_marker)
        for name in sorted(filenames):
            f = current / name
            suffix = f.suffix.lower()
            if suffix in HTML_SUFFIXES:
                leaks.extend(scan_html(f, str(f), guide, internal_marker))
            elif (suffix == ".json" and "search" in name.lower()) or suffix in TEXT_SUFFIXES:
                leaks.extend(_scan_text(f.read_text(errors="replace"), str(f), guide))
    return leaks


def scan_pdf(path: Path, guide: guidecfg.Guide, internal_marker: str = "internal") -> list[Leak]:
    """Leaks in a PDF: text per page (line = page number) and link annotation URIs."""
    from pypdf import PdfReader

    reader = PdfReader(str(path))
    label = str(path)
    leaks: list[Leak] = []
    for number, page in enumerate(reader.pages, 1):
        text = page.extract_text() or ""
        leaks.extend(_scan_text(text, label, guide, line_of=lambda _n, p=number: p))
        for annot in page.get("/Annots") or []:
            obj = annot.get_object()
            if obj.get("/Subtype") != "/Link":
                continue
            action = obj.get("/A")
            action = action.get_object() if action is not None else None
            uri = action.get("/URI") if action is not None else None
            if uri is not None and _is_internal_target(str(uri), internal_marker):
                leaks.append(Leak(label, number, "internal-link", str(uri)))
    return leaks


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Leak check for shareable sites and PDFs")
    parser.add_argument("--root", type=Path, default=Path("."), help="guide project root (for guide.yaml)")
    parser.add_argument("targets", nargs="+", type=Path)
    args = parser.parse_args(argv)
    try:
        guide = guidecfg.load_guide(args.root)
        adapter = guidecfg.load_adapter(args.root, required=False)
    except guidecfg.ConfigError as err:
        print(err)
        return 1
    marker = Path(adapter.internal_root).name if adapter else "internal"
    leaks: list[Leak] = []
    for target in args.targets:
        if target.is_dir():
            leaks.extend(scan_dir(target, guide, marker))
        elif target.is_file() and target.suffix.lower() == ".pdf":
            leaks.extend(scan_pdf(target, guide, marker))
        else:
            print(f"{target}: not found (build it first, for example with make site)")
            return 1
    for leak in leaks:
        print(leak)
    if leaks:
        print(f"leakcheck: {len(leaks)} leak(s)")
        return 1
    print("leakcheck: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
