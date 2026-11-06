#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Content checks for a guide project's pages (contracts/page-conventions.md).

Built-in global checks: typographic dashes, one sentence per line (AsciiDoc),
typed versions and commits, source blocks (and their resolution at the pinned
commits when .guide/cache exists), stubs (strict only), input references,
confidential terms and links into the internal part on shareable pages, image
alt texts with unquoted commas (AsciiDoc), and role=expand on Antora diagrams.
For a branded Antora guide (a guide.yaml `branding` block), the built views
(build/site, and build/site-internal while the internal part is enabled) must show
the branded header logo with its alt text on every page, and no footer may still
carry the Red Hat Demo Platform notice unless `footer` is `rhdp` (FR-032, rule
`branding`). With `theme: neutral`, every page of those views must also load
css/theme-neutral.css and no Google Fonts stylesheet, and _/font/ must hold the
vendored fonts and OFL.txt (specs/003-neutral-branding FR-026).
Per-guide rules come from the optional guide-rules.yaml:

  rules:
    - glob: content/modules/ROOT/pages/admin/*.adoc
      require: ["regex", {pattern: "regex", message: "why"}]
      forbid: ["regex"]
      require_order: ["first", "second"]

Usage: uv run tools/guide/check.py [--strict] [--root DIR]   (STRICT=1 also enables strict)
Exits 1 and prints `path:line: [rule] message` for every violation.
"""
from __future__ import annotations

import argparse
import html
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import yaml

import branding
import guidecfg
import sources

DASH_RE = re.compile("[" + chr(0x2013) + chr(0x2014) + "]")
TEXT_SUFFIXES = {".adoc", ".asciidoc", ".md", ".markdown", ".yml", ".yaml", ".txt", ".csv",
                 ".json", ".toml", ".html", ".htm", ".css", ".svg"}

# AsciiDoc structure
VERBATIM_DELIM_RE = re.compile(r"^(-{4,}|\.{4,}|\+{4,}|/{4,}|[|,:!]={3,}|`{3}.*)\s*$")
ATTR_ENTRY_RE = re.compile(r"^:[!\w][\w-]*!?:")
BLOCK_TITLE_RE = re.compile(r"^\.[^\s.]")
BLOCK_ATTR_RE = re.compile(r"^\[.*\]\s*$")
DIRECTIVE_RE = re.compile(r"^(include|ifdef|ifndef|ifeval|endif|image|toc|video|audio)::")
HEADING_RE = re.compile(r"^=+\s")
LIST_MARKER_RE = re.compile(r"^\s*(\*+|-|\.+|\d+\.|<\d+>|[A-Z]+:)\s+")
INLINE_CODE_RE = re.compile(r"`[^`]*`|\+[^+]+\+|https?://\S+")
BOUNDARY_RE = re.compile(r"([.!?])[\"')\]*_`]*\s+(?=[A-Z`*_\"'(\[])")
ABBREVIATIONS = {"e.g", "i.e", "etc", "vs", "cf", "approx", "fig", "no", "mr", "mrs", "ms", "dr",
                 "inc", "ltd", "st", "resp", "incl", "esp"}
IMAGE_RE = re.compile(r"image::?([^\s\[\]]+)\[([^\]]*)\]")
XREF_RE = sources.XREF_RE
MD_LINK_RE = sources.MD_LINK_RE
SCHEME_RE = sources.SCHEME_RE
STUB_LINE_RE = re.compile(r"^//\s*STUB\b")
STUB_HTML_RE = re.compile(r"^<!--\s*STUB\b.*-->\s*$")
# Generated regions (gfm adapter) hold baseline values written by attrs.py, not typed ones.
REGION_RE = re.compile(r"(<!--\s*guide:attr\s+[^\s>]+\s*-->).*?(<!--\s*/guide:attr\s*-->)")


@dataclass
class Violation:
    path: str
    line: int
    rule: str
    message: str

    def __str__(self) -> str:
        return f"{self.path}:{self.line}: [{self.rule}] {self.message}"


def _rel(path: Path, root: Path) -> str:
    try:
        return Path(path).resolve().relative_to(Path(root).resolve()).as_posix()
    except ValueError:
        return str(path)


def _verbatim_mask(lines: list[str]) -> list[bool]:
    """True for AsciiDoc lines inside (or delimiting) listing, literal, passthrough, comment, or table blocks."""
    mask = []
    open_delim: str | None = None
    for line in lines:
        stripped = line.rstrip()
        m = VERBATIM_DELIM_RE.match(stripped)
        if open_delim is None:
            if m:
                open_delim = _delim_kind(stripped)
                mask.append(True)
            else:
                mask.append(False)
        else:
            mask.append(True)
            if m and _delim_kind(stripped) == open_delim:
                open_delim = None
    return mask


def _delim_kind(line: str) -> str:
    if line.startswith("```"):
        return "```"
    if "===" in line:
        return line[0] + "==="
    return line[0] * 4


def _block_span(lines: list[str], block: sources.Block | None, style: str) -> set[int]:
    """1-based line numbers occupied by the page's source block."""
    if block is None or not block.start_line:
        return set()
    i = block.start_line - 1
    span = {i + 1}
    if style == "line":
        j = i + 1
        while j < len(lines) and lines[j].startswith("//") and not lines[j].startswith("////"):
            span.add(j + 1)
            j += 1
    else:
        j = i
        while j < len(lines):
            span.add(j + 1)
            if "-->" in lines[j]:
                break
            j += 1
    return span


def _typed_value_re(baseline: guidecfg.Baseline) -> re.Pattern | None:
    values: set[str] = set()
    for src in baseline.sources:
        if src.commit:
            values.add(src.commit)
            values.add(src.commit[:7])
        for value in src.attributes.values():
            value = str(value).strip()
            # values without a digit (names, branches) are not versions or commits
            if len(value) >= 3 and re.search(r"\d", value):
                values.add(value)
    if not values:
        return None
    alts = "|".join(re.escape(v) for v in sorted(values, key=len, reverse=True))
    return re.compile(rf"(?<![\w.-])v?(?:{alts})(?![\w-]|\.\w)")


def _sentence_problem(line: str) -> bool:
    """True when an AsciiDoc prose line holds more than one sentence."""
    text = LIST_MARKER_RE.sub("", line, count=1)
    text = INLINE_CODE_RE.sub("x", text)
    for m in BOUNDARY_RE.finditer(text):
        before = text[:m.start(1)]
        token = before.split()[-1] if before.split() else ""
        token = token.lstrip("(\"'`*_[").lower()
        if token in ABBREVIATIONS or (len(token) == 1 and token.isalpha()):
            continue
        return True
    return False


def _prose_line(line: str) -> bool:
    s = line.strip()
    if not s or s == "+":
        return False
    if s.startswith("//") or ATTR_ENTRY_RE.match(s) or BLOCK_TITLE_RE.match(s):
        return False
    if BLOCK_ATTR_RE.match(s) or DIRECTIVE_RE.match(s) or HEADING_RE.match(s):
        return False
    return True


def _split_attrlist(attrlist: str) -> list[str]:
    items, cur, quote = [], "", None
    for ch in attrlist:
        if quote:
            cur += ch
            if ch == quote:
                quote = None
        elif ch in "\"'":
            quote = ch
            cur += ch
        elif ch == ",":
            items.append(cur)
            cur = ""
        else:
            cur += ch
    items.append(cur)
    return items


def _alt_comma_unquoted(attrlist: str) -> bool:
    items = _split_attrlist(attrlist)
    first = items[0].strip()
    if not first or first[0] in "\"'":
        return False
    for item in items[1:]:
        item = item.strip()
        if not item or "=" in item:
            continue
        if re.fullmatch(r"\d+(%|px)?", item):
            continue
        return True
    return False


def _has_role_expand(attrlist: str) -> bool:
    return bool(re.search(r"(?:^|,)\s*role\s*=\s*[\"']?[^,\"']*\bexpand\b", attrlist))


def _antora_modules(root: Path, rel: str) -> set[str]:
    base = root / rel / "modules"
    return {p.name for p in base.iterdir() if p.is_dir()} if base.is_dir() else set()


def _xref_module(target: str) -> str | None:
    target = target.split("#", 1)[0]
    if "@" in target:
        target = target.split("@", 1)[1]
    parts = target.split(":")
    if len(parts) >= 3:
        return parts[-2] or None
    if len(parts) == 2:
        return parts[0] or None
    return None


def _text_files(root: Path, adapter: guidecfg.Adapter) -> list[Path]:
    found: set[Path] = set()
    for rel in (adapter.content_root, adapter.internal_root):
        base = root / rel
        if base.is_dir():
            found.update(p for p in base.rglob("*") if p.is_file() and p.suffix.lower() in TEXT_SUFFIXES)
    return sorted(found)


def _check_rules(root: Path) -> list[Violation]:
    path = root / "guide-rules.yaml"
    if not path.is_file():
        return []
    out: list[Violation] = []
    try:
        data = yaml.safe_load(path.read_text()) or {}
    except yaml.YAMLError as exc:
        return [Violation("guide-rules.yaml", 0, "rule-config", f"invalid YAML: {exc}")]
    rules = data.get("rules") or [] if isinstance(data, dict) else []
    if not isinstance(rules, list):
        return [Violation("guide-rules.yaml", 0, "rule-config", "rules must be a list")]
    for i, rule in enumerate(rules):
        key = f"rules[{i}]"
        if not isinstance(rule, dict) or "glob" not in rule:
            out.append(Violation("guide-rules.yaml", 0, "rule-config", f"{key}: needs a glob"))
            continue
        unknown = set(rule) - {"glob", "require", "forbid", "require_order", "message"}
        if unknown:
            out.append(Violation("guide-rules.yaml", 0, "rule-config", f"{key}: unknown keys {sorted(unknown)}"))
        files = sorted(p for p in root.glob(rule["glob"]) if p.is_file())
        if not files:
            out.append(Violation("guide-rules.yaml", 0, "rule-require", f"{key}: glob {rule['glob']!r} matches no file"))
            continue
        for f in files:
            rel = _rel(f, root)
            text = f.read_text(errors="replace")
            lines = text.splitlines()
            for entry in rule.get("require") or []:
                pattern, message = _rule_entry(entry, rule)
                if not re.search(pattern, text, re.M):
                    out.append(Violation(rel, 1, "rule-require", message or f"missing required pattern {pattern!r}"))
            for entry in rule.get("forbid") or []:
                pattern, message = _rule_entry(entry, rule)
                rx = re.compile(pattern)
                for n, line in enumerate(lines, 1):
                    if rx.search(line):
                        out.append(Violation(rel, n, "rule-forbid", message or f"forbidden pattern {pattern!r}"))
            order = rule.get("require_order") or []
            prev = 0
            for entry in order:
                pattern, _ = _rule_entry(entry, rule)
                rx = re.compile(pattern)
                hit = next((n for n, line in enumerate(lines, 1) if rx.search(line)), None)
                if hit is None:
                    out.append(Violation(rel, 1, "rule-order", f"missing {pattern!r}"))
                    break
                if hit < prev:
                    out.append(Violation(rel, hit, "rule-order", f"{pattern!r} appears before an earlier step"))
                    break
                prev = hit
    return out


def _rule_entry(entry, rule: dict) -> tuple[str, str]:
    if isinstance(entry, dict):
        return str(entry.get("pattern", "")), str(entry.get("message") or rule.get("message") or "")
    return str(entry), str(rule.get("message") or "")


FENCE_RE = re.compile(r"^\s*(`{3,}|~{3,})")


def _md_fence_mask(lines: list[str]) -> list[bool]:
    """True for lines inside fenced code blocks (Markdown)."""
    mask, fence = [], None
    for line in lines:
        m = FENCE_RE.match(line)
        if fence is None and m:
            fence = m.group(1)
            mask.append(True)
        elif fence is not None:
            mask.append(True)
            if m and m.group(1)[0] == fence[0] and len(m.group(1)) >= len(fence):
                fence = None
        else:
            mask.append(False)
    return mask


def check_structure(root: Path, guide: guidecfg.Guide, adapter: guidecfg.Adapter) -> list[Violation]:
    """Every persona's entry page exists and every section title is in the navigation."""
    root = Path(root)
    out: list[Violation] = []
    stems = {_rel(p, root).rsplit(".", 1)[0] for p in guidecfg.pages(root, adapter, "all")}
    for persona in guide.personas:
        start = persona.start.strip("/").rsplit(".", 1)[0] if persona.start.endswith((".adoc", ".md")) \
            else persona.start.strip("/")
        if not any(s == start or s.endswith("/" + start) for s in stems):
            out.append(Violation("guide.yaml", 0, "guide-structure",
                                 f"persona {persona.id!r}: entry page {persona.start!r} not found"))
    nav_files = list(root.glob(f"{adapter.content_root}/modules/*/nav.adoc")) + \
        [root / n for n in ("mkdocs.yml", "docs/SUMMARY.md", "docs/README.md") if (root / n).is_file()]
    nav = "\n".join(f.read_text(errors="replace") for f in nav_files)
    for section in guide.sections:
        if section.title and section.title not in nav:
            out.append(Violation("guide.yaml", 0, "guide-structure",
                                 f"section {section.id!r}: title {section.title!r} not in the navigation"))
    return out


IMG_TAG_RE = re.compile(r"<img\b[^>]*>", re.I)
ATTR_RE = re.compile(r'\b([\w-]+)\s*=\s*"([^"]*)"')
GOOGLE_FONTS = ("fonts.googleapis.com", "fonts.gstatic.com")   # the stock head-styles.hbs links
LINK_TAG_RE = re.compile(r"<link\b[^>]*>", re.I)
FOOTER_RE = re.compile(r'<footer\b[^>]*class="[^"]*\bfooter\b[^"]*"[^>]*>([\s\S]*?)</footer>', re.I)
STOCK_NOTICE = ("demo.redhat.com", "Demo Platform")
STOCK_NOTICE_MARKER = 'class="poweredBy"'          # the stock footer's link (contract rule footer-partial)
GENERATED_FOOTER_MARKER = 'class="guide-footer"'   # the footer rendered from branding.footer


def _stock_notice(footer: str, own_footer: bool) -> bool:
    """True when the content of a footer element is the stock Red Hat Demo Platform notice.

    The stock partial is recognized by its poweredBy link. Any other footer that names
    the Demo Platform counts too (for example the footer of a replaced UI bundle), except
    the one rendered from branding.footer and the author's own partials/footer-content.hbs
    in ui/supplemental/ (own_footer, passed only for a page whose header shows the branded
    logo, so the overlay demonstrably applied): both may mention the Demo Platform.
    """
    if STOCK_NOTICE_MARKER in footer:
        return True
    if own_footer or GENERATED_FOOTER_MARKER in footer:
        return False
    return any(notice in footer for notice in STOCK_NOTICE)


def _branding_problem(loc: str, msg: str) -> Violation:
    """A branding problem as a violation of the file it names (guide.yaml, site.yml,
    or the ui/supplemental/ entry)."""
    if loc == branding.OVERLAY or loc.startswith(branding.OVERLAY + ":"):
        rel = loc[len(branding.OVERLAY) + 1:]
        return Violation(f"{branding.OVERLAY}/{rel}" if rel else branding.OVERLAY, 0, "branding", msg)
    file, _, key = loc.partition(":")
    return Violation(file, 0, "branding", f"{key}: {msg}" if key else msg)


def check_branded_site(root: Path, guide: guidecfg.Guide) -> list[Violation]:
    """Built views of a branded guide that do not show the branding (FR-032).

    Every page with a header logo (img.navbar-logo-img) must show img/logo-guide.svg
    with the configured alt text (any alt text when ui/supplemental/ has its own
    helpers/navbar-logo-alt.js); a view where no page does (for example a replaced UI
    bundle that ignores the overlay) fails; unless footer is rhdp, no footer element may
    be the Red Hat Demo Platform notice (_stock_notice). Page content is not inspected,
    and pages under _exports/ (the print export) are skipped, as are views that were not
    built and the internal view while the internal part is disabled (its build/
    directory is then a leftover). An invalid branding block is reported as is, since
    the built views cannot be judged against it.
    """
    root = Path(root)
    if not isinstance(guide.branding, dict):
        return []
    raw, fmt = branding.load_block(root)
    problems, _ = branding.check(root, raw, fmt, branding.adapter_keys(root, fmt))
    if problems:
        return [_branding_problem(loc, msg) for loc, msg in problems]
    brand = branding.resolve(root, guide.branding)
    overlay = set(branding.overlay_files(root))
    own_alt = branding.ALT_HELPER in overlay
    own_footer = branding.FOOTER_PARTIAL in overlay
    views = ("site", "site-internal") if guide.internal_enabled else ("site",)
    out: list[Violation] = []
    for view in views:
        base = root / "build" / view
        pages = sorted(p for p in base.rglob("*.html") if "_exports" not in p.relative_to(base).parts) \
            if base.is_dir() else []
        if not pages:
            continue
        shown = 0
        for page in pages:
            rel = f"build/{view}/{page.relative_to(base).as_posix()}"
            text = page.read_text(errors="replace")
            branded = False
            for tag in IMG_TAG_RE.findall(text):
                attrs = dict(ATTR_RE.findall(tag))
                if "navbar-logo-img" not in attrs.get("class", "").split():
                    continue
                src, alt = attrs.get("src", ""), html.unescape(attrs.get("alt", ""))
                if src.endswith(branding.LOGO_FILE) and (own_alt or alt == brand.alt):
                    shown += 1
                    branded = True
                else:
                    out.append(Violation(rel, 0, "branding",
                                         f"header does not show the branded logo (found {src}, alt {alt!r})"))
                break
            if brand.footer != "rhdp":
                for footer in FOOTER_RE.findall(text):
                    if _stock_notice(footer, own_footer and branded):
                        out.append(Violation(rel, 0, "branding", "Red Hat Demo Platform notice still present"))
                        break
        if not shown:
            out.append(Violation(f"build/{view}", 0, "branding", "no page shows the branded logo"))
    return out


def _shows_logo(tag: str) -> bool:
    return "navbar-logo-img" in dict(ATTR_RE.findall(tag)).get("class", "").split()


def check_neutral_theme(root: Path, guide: guidecfg.Guide) -> list[Violation]:
    """Built views of a guide with `theme: neutral` that do not carry the theme (FR-026).

    Every page with a header logo (a UI page; Antora's start page redirect has none)
    must link css/theme-neutral.css and no Google Fonts host, and build/<view>/_/font/
    must hold the vendored fonts and OFL.txt. A view where no page has the header (a UI
    bundle that ignores the overlay) is reported by check_branded_site ("no page shows
    the branded logo"), not twice. Views, skipped pages, the internal view, and an invalid
    branding block (also reported there) follow check_branded_site. An author's ui/supplemental/css/theme-neutral.css counts, since
    the generated head-meta.hbs still links it.
    """
    root = Path(root)
    if not isinstance(guide.branding, dict) or guide.branding.get("theme") != "neutral":
        return []
    raw, fmt = branding.load_block(root)
    problems, _ = branding.check(root, raw, fmt, branding.adapter_keys(root, fmt))
    if problems:
        return []
    names = branding.font_files()
    views = ("site", "site-internal") if guide.internal_enabled else ("site",)
    out: list[Violation] = []
    for view in views:
        base = root / "build" / view
        pages = sorted(p for p in base.rglob("*.html") if "_exports" not in p.relative_to(base).parts) \
            if base.is_dir() else []
        if not pages:
            continue
        for page in pages:
            text = page.read_text(errors="replace")
            if not any(_shows_logo(tag) for tag in IMG_TAG_RE.findall(text)):
                continue
            links = [dict(ATTR_RE.findall(tag)).get("href", "") for tag in LINK_TAG_RE.findall(text)]
            rel = f"build/{view}/{page.relative_to(base).as_posix()}"
            if not any(href.endswith(branding.THEME_CSS) for href in links):
                out.append(Violation(rel, 0, "branding", "page does not load the neutral theme"))
            if any(host in href for href in links for host in GOOGLE_FONTS):
                out.append(Violation(rel, 0, "branding", "page still loads Google Fonts"))
        for name in names:
            if not (base / "_" / branding.FONT_DIR / name).is_file():
                out.append(Violation(f"build/{view}", 0, "branding", f"neutral theme font missing: {name}"))
    return out


def check_pages(root: Path, guide: guidecfg.Guide, baseline: guidecfg.Baseline,
                strict: bool = False, structure: bool = False) -> list[Violation]:
    """All content-check violations of a guide project."""
    root = Path(root)
    adapter = guidecfg.load_adapter(root)
    style = "line" if adapter.comment == "line" else "html"
    out: list[Violation] = []
    try:
        inputs = guidecfg.load_inputs(root)
    except guidecfg.ConfigError as err:
        out.extend(Violation("inputs.yaml", 0, "config", line) for line in err.lines())
        inputs = guidecfg.Inputs(root=root)

    cache = root / ".guide" / "cache"
    typed = _typed_value_re(baseline)
    terms = [(t, re.compile(re.escape(t), re.I)) for t in guide.confidential_terms]
    patterns = [re.compile(p) for p in guide.confidential_patterns]
    diagrams = {p.stem for p in (root / "diagrams").glob("*.excalidraw")} if (root / "diagrams").is_dir() else set()
    internal_dir = (root / adapter.internal_root).resolve()
    internal_modules: set[str] = set()
    if adapter.format == "antora":
        internal_modules = (_antora_modules(root, adapter.internal_root)
                            - _antora_modules(root, adapter.content_root)) or {"internal"}

    for page in guidecfg.pages(root, adapter, "all"):
        rel = _rel(page, root)
        text = page.read_text(errors="replace")
        lines = text.splitlines()
        internal = guidecfg.is_internal(page, root, adapter)
        is_adoc = page.suffix in (".adoc", ".asciidoc")
        block = sources.find_block(text, style)
        span = _block_span(lines, block, style)
        mask = _verbatim_mask(lines) if is_adoc else [False] * len(lines)
        fenced = _md_fence_mask(lines) if not is_adoc else [False] * len(lines)

        for p in sources.check_text(text, style, rel):
            out.append(Violation(rel, p.line, "source-block", p.message))
        for ref in (block.refs if block else []):
            if ref.kind == "input":
                if not internal:
                    out.append(Violation(rel, ref.line, "input-on-shareable",
                                         f"{ref.raw}: input references are allowed only on internal pages"))
                elif inputs.get(ref.input_id) is None:
                    out.append(Violation(rel, ref.line, "unknown-input",
                                         f"unknown input {ref.input_id!r} (not in inputs.yaml)"))
            elif ref.kind == "source" and cache.is_dir():
                res = sources.resolve(ref, baseline, cache, inputs)
                if not res.ok:
                    out.append(Violation(rel, ref.line, "source-ref", res.message))

        stub_re = STUB_LINE_RE if style == "line" else STUB_HTML_RE
        for n, line in enumerate(lines, 1):
            in_block = n in span
            if strict and stub_re.match(line.strip()):
                out.append(Violation(rel, n, "stub", "unfinished stub page"))
            if typed and not in_block:
                for m in typed.finditer(REGION_RE.sub(r"\1\2", line)):
                    out.append(Violation(rel, n, "typed-value",
                                         f"typed version or commit {m.group(0)!r}; use a baseline attribute"))
            if is_adoc and not mask[n - 1] and not in_block and _prose_line(line) and _sentence_problem(line):
                out.append(Violation(rel, n, "sentence-per-line", "more than one sentence on this line"))
            if not internal:
                for term, rx in terms:
                    if rx.search(line):
                        out.append(Violation(rel, n, "confidential", f"confidential term {term!r} on a shareable page"))
                for rx in patterns:
                    m = rx.search(line)
                    if m:
                        out.append(Violation(rel, n, "confidential",
                                             f"confidential pattern match {m.group(0)!r} on a shareable page"))
                if is_adoc:
                    for m in XREF_RE.finditer(line):
                        if _xref_module(m.group(1)) in internal_modules:
                            out.append(Violation(rel, n, "internal-link", f"xref:{m.group(1)} links into the internal part"))
                else:
                    for m in MD_LINK_RE.finditer(line):
                        target = (m.group(1) or m.group(2) or "").split("#", 1)[0].split("?", 1)[0]
                        if not target or SCHEME_RE.match(target) or target.startswith("/"):
                            continue
                        resolved = (page.parent / target).resolve()
                        if resolved == internal_dir or internal_dir in resolved.parents:
                            out.append(Violation(rel, n, "internal-link", f"{target} links into the internal part"))
            if not is_adoc and not fenced[n - 1]:
                for m in MD_LINK_RE.finditer(line):
                    target = (m.group(1) or m.group(2) or "").split("#", 1)[0].split("?", 1)[0]
                    if not target or SCHEME_RE.match(target) or target.startswith("/") or "{{" in target:
                        continue
                    resolved = (page.parent / target).resolve()
                    if not internal and (resolved == internal_dir or internal_dir in resolved.parents):
                        continue  # reported as internal-link
                    if not resolved.exists():
                        out.append(Violation(rel, n, "broken-link", f"{target} does not exist"))
            if is_adoc and not mask[n - 1] and not line.lstrip().startswith("//"):
                for m in IMAGE_RE.finditer(line):
                    target, attrlist = m.group(1), m.group(2)
                    if _alt_comma_unquoted(attrlist):
                        out.append(Violation(rel, n, "image-alt",
                                             f"alt text of {target} contains a comma; quote it: [\"...\"]"))
                    if adapter.format == "antora" and Path(target).stem in diagrams and not _has_role_expand(attrlist):
                        out.append(Violation(rel, n, "diagram-expand",
                                             f"diagram {target} needs role=expand so it opens enlarged on click"))

    for f in _text_files(root, adapter):
        try:
            text = f.read_text()
        except UnicodeDecodeError:
            continue
        for n, line in enumerate(text.splitlines(), 1):
            if DASH_RE.search(line):
                out.append(Violation(_rel(f, root), n, "dash", "em dash or en dash character; rephrase"))

    out.extend(_check_rules(root))
    if adapter.format == "antora" and guide.branding is not None:
        out.extend(check_branded_site(root, guide))
        out.extend(check_neutral_theme(root, guide))
    if structure:
        out.extend(check_structure(root, guide, adapter))
    return out


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Content checks for a guide project")
    parser.add_argument("--strict", action="store_true", help="also fail on stub pages (strict rigor)")
    parser.add_argument("--structure", action="store_true",
                        help="also check that persona entry pages and section titles resolve (end of a run)")
    parser.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args(argv)
    strict = args.strict or os.environ.get("STRICT", "") == "1"
    structure = args.structure or os.environ.get("STRUCTURE", "") == "1"
    try:
        guide = guidecfg.load_guide(args.root)
        baseline = guidecfg.load_baseline(args.root)
        violations = check_pages(args.root, guide, baseline, strict, structure)
    except guidecfg.ConfigError as err:
        print(err)
        return 1
    for v in violations:
        print(v)
    if violations:
        print(f"check: {len(violations)} violation(s)")
        return 1
    print("check: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
