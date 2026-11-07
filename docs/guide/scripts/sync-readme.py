#!/usr/bin/env python3
"""Publish the guide introduction at the repository root with rebased links."""

import argparse
import os
from pathlib import Path
import re
import sys
from urllib.parse import urlsplit, urlunsplit

GUIDE = Path(__file__).resolve().parents[1]
REPO = GUIDE.parent.parent
SOURCE = GUIDE / "docs/README.md"
OUTPUT = REPO / "README.md"

# Inline links/images and reference definitions, with optional angle brackets.
DEST = r"(<[^>\n]+>|(?:[^\s()<>]|\([^()\n]*\))+)"
INLINE = re.compile(r"(\]\(\s*)" + DEST)
REFERENCE = re.compile(r"(^ {0,3}\[[^\]\n]+\]:\s*)" + DEST)
CODE = re.compile(r"(`+).*?\1")
FENCE = re.compile(r"^ {0,3}(`{3,}|~{3,})")


def rebase(match: re.Match) -> str:
    prefix, target = match.groups()
    angled = target.startswith("<")
    url = target[1:-1] if angled else target
    parts = urlsplit(url)
    if parts.scheme or parts.netloc or not parts.path or url.startswith("/"):
        return match.group(0)
    path = os.path.relpath(SOURCE.parent / parts.path, REPO)
    url = urlunsplit(("", "", path, parts.query, parts.fragment))
    return prefix + (f"<{url}>" if angled else url)


def render(source: str) -> str:
    lines = []
    fence = None
    for line in source.splitlines(keepends=True):
        marker = FENCE.match(line)
        if marker:
            token = marker.group(1)
            if fence is None:
                fence = token
            elif token[0] == fence[0] and len(token) >= len(fence):
                fence = None
            lines.append(line)
        elif fence:
            lines.append(line)
        else:
            # Protect inline code so Markdown examples remain literal.
            code = []

            def protect(match):
                code.append(match.group(0))
                return f"\x00{len(code) - 1}\x00"

            line = CODE.sub(protect, line)
            line = INLINE.sub(rebase, line)
            line = REFERENCE.sub(rebase, line)
            line = re.sub(r"\x00(\d+)\x00", lambda m: code[int(m[1])], line)
            lines.append(line)
    header = (
        "<!-- Generated from docs/guide/docs/README.md. Edit that file and run\n"
        "     make -C docs/guide readme from the repository root. -->\n\n"
    )
    footer = "\n## Project information\n\n[Project details and licensing](docs/README.md).\n"
    return header + "".join(lines).rstrip() + "\n" + footer


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--write", action="store_true")
    mode.add_argument("--check", action="store_true")
    args = parser.parse_args()
    expected = render(SOURCE.read_text())
    if args.write:
        if not OUTPUT.exists() or OUTPUT.read_text() != expected:
            OUTPUT.write_text(expected)
        print("README.md: up to date")
        return 0
    if not OUTPUT.exists() or OUTPUT.read_text() != expected:
        print("README.md is stale; run make -C docs/guide readme", file=sys.stderr)
        return 1
    print("README.md: up to date")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
