#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml", "fonttools[woff]==4.67.0"]
# ///
"""Branding for Antora guides: schema checks, overlay rendering, and the UI bundle contract.

A guide.yaml `branding` block and the optional overlay directory ui/supplemental/
become, at build time, one merged UI overlay per view in build/branding/<view>/ui/.
tools/guide/playbook.py passes it to Antora as ui.supplemental_files, on top of the
pinned, unmodified UI bundle. Nothing outside build/ is written.

  branding:
    logo_text: Agent Starter Kits        # or wordmark: <text> (no Red Hat hat), or
                                         # logo: branding/logo.svg (then logo_alt is required)
    logo_alt: Red Hat Agent Starter Kits # default: "Red Hat <logo_text>", or the wordmark
    footer: {text: ..., url: https://...} # rhdp | none | {text, url}; default none
    favicon: branding/favicon.png        # .ico, .png, or .svg
    color: "#3e8635"                     # replaces the bundle's accent blue
    theme: neutral                       # neutral | stock (default): vendored Red Hat
                                         # fonts, a red navbar rule, outlined logo text

Only the keys the guide's adapter lists in tools/guide/adapter.yaml (`branding:`) are
accepted; the Antora adapter lists all eight, MkDocs and gfm none. The logo template,
the bundle contract, and the fonts (fonts/, with the manifest fonts.yaml) come from
tools/guide/branding/ (vendored by scaffold-project.sh), or from
guide/adapters/antora/branding/ inside the plugin source tree.

Usage:
  uv run tools/guide/branding.py validate [--root DIR]
      one `location: message` line per problem (exit 1), then `location: warning: ...` lines
  uv run tools/guide/branding.py render --view shareable|internal [--root DIR]
      renders build/branding/<view>/ and prints its ui/ path
  uv run tools/guide/branding.py contract --bundle <ui-bundle.zip|dir> [--contract FILE]
      checks a UI bundle against contract.yaml; one line per failing rule (exit 1)
"""
from __future__ import annotations

import argparse
import hashlib
import html
import json
import math
import os
import re
import shutil
import sys
import urllib.parse
import zipfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any

import yaml

KEYS = ("logo_text", "wordmark", "logo", "logo_alt", "footer", "favicon", "color", "theme")
LOGO_KEYS = ("logo_text", "wordmark", "logo")
THEMES = ("neutral", "stock")
WORDMARK_WEIGHT = 500       # Red Hat Display Medium for the wordmark
LOGO_TEXT_WEIGHT = 700      # the logo template's font-weight
# Used when tools/guide/adapter.yaml is missing (for example in tests); other formats: [].
DEFAULT_KEYS = {"antora": list(KEYS)}

LOGO_TEXT_MAX = 40
LOGO_SUFFIXES = (".svg",)
FAVICON_TYPES = {".ico": "image/x-icon", ".png": "image/png", ".svg": "image/svg+xml"}
FOOTERS = ("rhdp", "none")
COLOR_RE = re.compile(r"#[0-9a-fA-F]{6}")    # used with fullmatch
VIEWS = ("shareable", "internal")
PLAYBOOKS = ("site.yml", "site-internal.yml")
OVERLAY = "ui/supplemental"

# Files of the merged overlay (paths relative to its ui/ directory).
LOGO_FILE = "img/logo-guide.svg"
ALT_HELPER = "helpers/navbar-logo-alt.js"
FOOTER_PARTIAL = "partials/footer-content.hbs"
HEAD_META = "partials/head-meta.hbs"
HEAD_ICONS = "partials/head-icons.hbs"
BRANDING_CSS = "css/branding.css"
EXTRA_CSS = "css/extra.css"
FAVICON_STEM = "img/favicon-guide"
THEME_CSS = "css/theme-neutral.css"
HEAD_STYLES = "partials/head-styles.hbs"
FONT_DIR = "font"
MANIFEST = "fonts.yaml"
OFL = "OFL.txt"

BLOCK = "guide.yaml:branding"
FOOTER_MSG = "must be rhdp, none, or a mapping with text and optional url"
INSIDE_MSG = "must be a path inside the guide project (got {})"
LOGO_TEXT_MSG = f"must be one line of 1 to {LOGO_TEXT_MAX} characters (got {{}})"
LINE_MSG = "must be one non-empty line of text"
REFRESH = "refresh the toolkit (scaffold-project.sh --toolkit-only)"

Pairs = list[tuple[str, str]]


def asset_dir() -> Path:
    """The logo template and the bundle contract: tools/guide/branding/ in a guide
    project, guide/adapters/antora/branding/ in the plugin source tree."""
    here = Path(__file__).resolve().parent
    for candidate in (here / "branding", here.parent / "adapters" / "antora" / "branding"):
        if candidate.is_dir():
            return candidate
    raise FileNotFoundError(f"branding assets not found in {here / 'branding'}; refresh the toolkit "
                            "(scaffold-project.sh --toolkit-only)")


def fonts_dir() -> Path:
    """The vendored Red Hat fonts and their manifest (asset_dir() / "fonts")."""
    return asset_dir() / "fonts"


FONT_FILE_RE = re.compile(r"[A-Za-z0-9._-]+\.woff2")
RELEASE_RE = re.compile(r"[0-9A-Za-z]+(?:[._-][0-9A-Za-z]+)*")   # never "--" (an SVG comment)
SHA256_RE = re.compile(r"[0-9a-f]{64}")
FAMILIES = ("Red Hat Display", "Red Hat Text", "Red Hat Mono")
MANIFEST_ERRORS = (OSError, ValueError, yaml.YAMLError)


def _is_int(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _validate_manifest(data: Any) -> None:
    """Raise ValueError unless data has the manifest shape. Its values reach file paths,
    generated CSS, an SVG comment, and the PDF printer, so each one is checked."""
    if not isinstance(data, dict):
        raise ValueError("not a mapping")
    if not isinstance(data.get("release"), str) or not RELEASE_RE.fullmatch(data["release"]):
        raise ValueError("release must be a release tag")
    files = data.get("files")
    if not isinstance(files, list) or not files:
        raise ValueError("files must be a non-empty list")
    for i, entry in enumerate(files):
        weight = entry.get("weight") if isinstance(entry, dict) else None
        if not (isinstance(entry, dict)
                and isinstance(entry.get("file"), str) and FONT_FILE_RE.fullmatch(entry["file"])
                and isinstance(entry.get("sha256"), str) and SHA256_RE.fullmatch(entry["sha256"])
                and entry.get("family") in FAMILIES and entry.get("style") in ("normal", "italic")
                and isinstance(weight, list) and len(weight) == 2 and all(map(_is_int, weight))):
            raise ValueError(f"files[{i}] is malformed")
    coverage = data.get("display_coverage")
    if not isinstance(coverage, list) or not all(
            isinstance(r, list) and len(r) == 2 and all(map(_is_int, r)) for r in coverage):
        raise ValueError("display_coverage must be a list of [first, last] code point pairs")


def load_manifest(path: Path | None = None) -> dict:
    """The font manifest (fonts.yaml of fonts_dir() unless path is given). Raises one of
    MANIFEST_ERRORS when it is missing, unreadable, or malformed."""
    data = yaml.safe_load(Path(path or fonts_dir() / MANIFEST).read_text())
    _validate_manifest(data)
    return data


def font_files(manifest: dict | None = None) -> list[str]:
    """The overlay's font/ files: the manifest's fonts, then OFL.txt."""
    manifest = load_manifest() if manifest is None else manifest
    return [entry["file"] for entry in manifest["files"]] + [OFL]


def missing_glyphs(text: str, manifest: dict | None = None) -> list[str]:
    """Characters of text (in order, without repeats) Red Hat Display has no glyph for."""
    manifest = load_manifest() if manifest is None else manifest
    ranges = manifest.get("display_coverage") or []
    out: list[str] = []
    for ch in text:
        cp = ord(ch)
        if ch not in out and not any(lo <= cp <= hi for lo, hi in ranges):
            out.append(ch)
    return out


def _font_location(root: Path, path: Path) -> str:
    """A font path for messages: relative to the guide root when inside it, else relative
    to the toolkit's grandparent (the guide root in a project, the repository in the
    plugin source tree)."""
    path = Path(path).resolve()
    for base in (Path(root).resolve(), Path(__file__).resolve().parents[2]):
        if _inside(path, base):
            return path.relative_to(base).as_posix()
    return str(path)


def check_fonts(root: Path) -> Pairs:
    """Missing, unreadable, or changed vendored fonts: the branding directory, the
    manifest, the manifest's files, then OFL.txt. Every problem asks for a toolkit refresh."""
    try:
        fonts = fonts_dir()
    except FileNotFoundError:
        return [("tools/guide/branding", f"missing; {REFRESH}")]
    loc = _font_location(root, fonts / MANIFEST)
    if not (fonts / MANIFEST).is_file():
        return [(loc, f"missing; {REFRESH}")]
    try:
        manifest = load_manifest()
    except MANIFEST_ERRORS as err:
        detail = str(err).strip().splitlines()[0] if str(err).strip() else type(err).__name__
        return [(loc, f"unreadable ({detail}); {REFRESH}")]
    problems: Pairs = []
    for entry in manifest["files"]:
        path = fonts / entry["file"]
        if not path.is_file():
            problems.append((_font_location(root, path), f"missing; {REFRESH}"))
        elif hashlib.sha256(path.read_bytes()).hexdigest() != entry["sha256"]:
            problems.append((_font_location(root, path), f"checksum mismatch; {REFRESH}"))
    if not (fonts / OFL).is_file():
        problems.append((_font_location(root, fonts / OFL), f"missing; {REFRESH}"))
    return problems


# --- validation ----------------------------------------------------------------


def _inside(path: Path, root: Path) -> bool:
    return path == root or root in path.parents


def _one_line(value: Any) -> str | None:
    """The trimmed text when value is one non-empty line, else None."""
    if not isinstance(value, str):
        return None
    text = value.strip()
    return text if text and len(text.splitlines()) == 1 else None


def _http_url(value: Any) -> bool:
    if not isinstance(value, str) or any(c.isspace() for c in value):
        return False
    parts = urllib.parse.urlsplit(value)
    return parts.scheme.lower() in ("http", "https") and bool(parts.hostname)


def _check_path(root: Path, key: str, value: Any, suffixes, suffix_msg: str, problems: Pairs) -> None:
    loc = f"{BLOCK}.{key}"
    if not isinstance(value, str) or not value.strip():
        problems.append((loc, INSIDE_MSG.format(value)))
        return
    rel = PurePosixPath(value)
    if Path(value).is_absolute() or value.startswith("~") or ".." in rel.parts:
        problems.append((loc, INSIDE_MSG.format(value)))
        return
    if rel.suffix.lower() not in suffixes:
        problems.append((loc, f"{suffix_msg} (got {value})"))
    path = root / value
    if not path.is_file():
        problems.append((loc, f"file not found: {value}"))
        return
    if not _inside(path.resolve(), root.resolve()):
        problems.append((loc, INSIDE_MSG.format(value)))


def _check_block(root: Path, raw: Any, fmt: str, adapter_keys: list[str], problems: Pairs) -> None:
    if not isinstance(raw, dict):
        problems.append((BLOCK, "must be a mapping"))
        return
    if not adapter_keys:
        # The format supports no branding at all: name the format for every key.
        if not raw:
            problems.append((BLOCK, f"not supported by the {fmt} adapter"))
        for key in raw:
            problems.append((f"{BLOCK}.{key}", f"not supported by the {fmt} adapter"))
        return
    for key in raw:
        if key not in KEYS:
            problems.append((f"{BLOCK}.{key}", "unknown key"))
    unsupported = [k for k in raw if k in KEYS and k not in adapter_keys]
    for key in unsupported:
        problems.append((f"{BLOCK}.{key}", f"not supported by the {fmt} adapter"))
    if unsupported:
        return

    def present(key: str) -> bool:
        return raw.get(key) is not None

    has_text, has_mark, has_logo = (present(key) for key in LOGO_KEYS)   # LOGO_KEYS order
    if sum((has_text, has_logo, has_mark)) != 1:
        problems.append((BLOCK, "set exactly one of logo_text, wordmark, or logo"))
    texts: dict[str, str] = {}
    for key in ("logo_text", "wordmark"):
        if not present(key):
            continue
        value = raw[key]
        if isinstance(value, bool) or not isinstance(value, (str, int, float)):
            problems.append((f"{BLOCK}.{key}", LOGO_TEXT_MSG.format(f"a {type(value).__name__}")))
        else:
            text = str(value).strip()
            if not text or len(text) > LOGO_TEXT_MAX or len(text.splitlines()) > 1:
                problems.append((f"{BLOCK}.{key}", LOGO_TEXT_MSG.format(len(text))))
            else:
                texts[key] = text
    if has_logo:
        _check_path(root, "logo", raw["logo"], LOGO_SUFFIXES, "must be an .svg file", problems)
    alt = raw.get("logo_alt")
    if has_logo and (alt is None or (isinstance(alt, str) and not alt.strip())):
        problems.append((f"{BLOCK}.logo_alt", "required with branding.logo"))
    elif alt is not None and _one_line(alt) is None:
        problems.append((f"{BLOCK}.logo_alt", LINE_MSG))

    footer = raw.get("footer")
    loc = f"{BLOCK}.footer"
    if isinstance(footer, str):
        if footer not in FOOTERS:
            problems.append((loc, FOOTER_MSG))
    elif isinstance(footer, dict):
        for key in footer:
            if key not in ("text", "url"):
                problems.append((f"{loc}.{key}", "unknown key"))
        text = footer.get("text")
        if text is None or text == "":
            problems.append((loc, FOOTER_MSG))
        elif _one_line(text) is None:
            problems.append((f"{loc}.text", LINE_MSG))
        if footer.get("url") is not None and not _http_url(footer.get("url")):
            problems.append((f"{loc}.url", "must be an absolute http or https URL"))
    elif footer is not None:
        problems.append((loc, FOOTER_MSG))

    if present("favicon"):
        _check_path(root, "favicon", raw["favicon"], tuple(FAVICON_TYPES),
                    "must be an .ico, .png, or .svg file", problems)

    if "color" in raw:
        color = raw["color"]
        if color is None:
            problems.append((f"{BLOCK}.color", 'must be a #rrggbb color (got no value; quote it, as in color: "#3e8635")'))
        elif not isinstance(color, str) or not COLOR_RE.fullmatch(color):
            problems.append((f"{BLOCK}.color", f"must be a #rrggbb color (got {color})"))

    neutral = raw.get("theme") == "neutral"
    if "theme" in raw and raw["theme"] not in THEMES:
        theme = raw["theme"]
        got = "null" if theme is None else '""' if theme == "" else str(theme)
        problems.append((f"{BLOCK}.theme", f"must be neutral or stock (got {got})"))
    # Outlined text needs the vendored fonts: the wordmark always, logo_text under neutral.
    outlined = {k: v for k, v in texts.items() if k == "wordmark" or neutral}
    if neutral or has_mark:
        problems.extend(check_fonts(root))
        try:
            manifest = load_manifest()
        except MANIFEST_ERRORS:
            manifest = None             # check_fonts reported it
        if outlined and manifest is not None:
            for key, text in outlined.items():
                for ch in missing_glyphs(text, manifest):
                    problems.append((f"{BLOCK}.{key}",
                                     f'no glyph for "{ch}" (U+{ord(ch):04X}) in Red Hat Display'))


def overlay_files(root: Path, problems: Pairs | None = None) -> list[str]:
    """Regular files of ui/supplemental/ as sorted POSIX paths relative to it.

    Problems (not a directory, links outside the project, non-regular files) are
    appended to problems when given; such entries are left out of the result.
    """
    root = Path(root)
    problems = [] if problems is None else problems
    overlay = root / OVERLAY
    if not os.path.lexists(overlay):
        return []
    real_root = root.resolve()
    if not overlay.is_dir():
        problems.append((OVERLAY, "must be a directory"))
        return []
    if not _inside(overlay.resolve(), real_root):
        problems.append((OVERLAY, "links outside the guide project"))
        return []
    files: list[str] = []
    for base, dirs, names in os.walk(overlay):
        base_path = Path(base)
        for name in sorted(dirs):
            path = base_path / name
            if path.is_symlink():
                rel = path.relative_to(overlay).as_posix()
                reason = ("links outside the guide project" if not _inside(path.resolve(), real_root)
                          else "is a link to a directory; use a regular directory")
                problems.append((f"{OVERLAY}:{rel}", reason))
                dirs.remove(name)
        dirs.sort()
        for name in sorted(names):
            path = base_path / name
            rel = path.relative_to(overlay).as_posix()
            if path.is_symlink() and not _inside(path.resolve(), real_root):
                problems.append((f"{OVERLAY}:{rel}", "links outside the guide project"))
            elif not path.is_file():
                problems.append((f"{OVERLAY}:{rel}", "must be a regular file"))
            else:
                files.append(rel)
    return sorted(files)


def generated_files(raw: Any, extra_css: bool) -> dict[str, str]:
    """Overlay paths the renderer generates, mapped to the setting that produces them."""
    out: dict[str, str] = {}
    if isinstance(raw, dict):
        logo_key = ("wordmark" if raw.get("wordmark") is not None
                    else "logo" if raw.get("logo") is not None and raw.get("logo_text") is None
                    else "logo_text")
        out[LOGO_FILE] = f"branding.{logo_key}"
        out[ALT_HELPER] = "branding.logo_alt"
        if raw.get("footer") != "rhdp":
            out[FOOTER_PARTIAL] = "branding.footer"
        favicon = raw.get("favicon")
        suffix = PurePosixPath(favicon).suffix.lower() if isinstance(favicon, str) else ""
        if suffix in FAVICON_TYPES:
            out[f"{FAVICON_STEM}{suffix}"] = "branding.favicon"
            out[HEAD_ICONS] = "branding.favicon"
        if raw.get("color") is not None:
            out[BRANDING_CSS] = "branding.color"
            out[HEAD_META] = "branding.color"
        elif suffix in FAVICON_TYPES:
            out[HEAD_META] = "branding.favicon"
        if raw.get("theme") == "neutral":
            out[THEME_CSS] = "branding.theme"
            out[HEAD_STYLES] = "branding.theme"
            try:
                names = font_files()
            except MANIFEST_ERRORS:
                names = []              # check_fonts reports it
            for name in names:
                out[f"{FONT_DIR}/{name}"] = "branding.theme"
            out.setdefault(HEAD_META, "branding.theme")
    if extra_css and HEAD_META not in out:
        out[HEAD_META] = f"{OVERLAY}/{EXTRA_CSS}"
    return out


def _check_playbooks(root: Path, problems: Pairs) -> None:
    for name in PLAYBOOKS:
        path = root / name
        if not path.is_file():
            continue
        try:
            data = yaml.safe_load(path.read_text()) or {}
        except yaml.YAMLError:
            continue
        ui = data.get("ui") if isinstance(data, dict) else None
        if isinstance(ui, dict) and ui.get("supplemental_files") is not None:
            problems.append((f"{name}:ui.supplemental_files", "already set; move these files into ui/supplemental/"))


def check(root: Path, raw: Any, fmt: str, adapter_keys: list[str]) -> tuple[Pairs, Pairs]:
    """Problems and warnings of a branding block (raw, None when absent) and the overlay.

    Both are (location, message) pairs; a problem prints as `location: message`, a
    warning as `location: warning: message`. The overlay and playbook checks run for
    the antora format only.
    """
    root = Path(root)
    problems: Pairs = []
    warnings: Pairs = []
    if raw is not None:
        _check_block(root, raw, fmt, adapter_keys, problems)
    if fmt != "antora":
        return problems, warnings
    files = overlay_files(root, problems)
    if raw is not None or os.path.lexists(root / OVERLAY):
        _check_playbooks(root, problems)
    generated = generated_files(raw, EXTRA_CSS in files)
    for rel in files:
        if rel in generated:
            warnings.append((f"{OVERLAY}/{rel}", f"shadows the file generated for {generated[rel]}"))
    return problems, warnings


def _read_yaml(path: Path) -> Any:
    try:
        return yaml.safe_load(path.read_text())
    except (OSError, yaml.YAMLError):
        return None


def load_block(root: Path) -> tuple[Any, str]:
    """The raw branding block of guide.yaml (None when absent) and the guide's format."""
    data = _read_yaml(Path(root) / "guide.yaml")
    if not isinstance(data, dict):
        return None, "antora"
    fmt = data.get("format")
    return data.get("branding"), fmt if isinstance(fmt, str) else "antora"


def adapter_keys(root: Path, fmt: str) -> list[str]:
    """Branding keys of the vendored adapter descriptor (DEFAULT_KEYS without one)."""
    path = Path(root) / "tools" / "guide" / "adapter.yaml"
    if not path.is_file():
        return list(DEFAULT_KEYS.get(fmt, []))
    data = _read_yaml(path)
    keys = data.get("branding") if isinstance(data, dict) else None
    return [str(k) for k in keys] if isinstance(keys, list) else []


def check_guide(root: Path) -> tuple[list[str], list[str]]:
    """Formatted problem and warning lines for the guide project at root.

    Reads guide.yaml (a missing or unreadable file gives no lines; guidecfg reports it)
    and the adapter keys from tools/guide/adapter.yaml.
    """
    root = Path(root)
    if not (root / "guide.yaml").is_file():
        return [], []
    raw, fmt = load_block(root)
    problems, warnings = check(root, raw, fmt, adapter_keys(root, fmt))
    return [f"{loc}: {msg}" for loc, msg in problems], [f"{loc}: warning: {msg}" for loc, msg in warnings]


# --- resolved branding -----------------------------------------------------------


@dataclass
class Resolved:
    logo_text: str | None
    logo: Path | None
    alt: str
    footer: str | dict          # "rhdp", "none", or {"text": str, "url": str | None}
    favicon: Path | None
    color: str | None
    hover: str | None
    wordmark: str | None = None
    theme: str = "stock"


def hover_shade(color: str) -> str:
    """The link hover shade of an accent color: each channel times 0.75, rounded half up.

    The stock pair is #0066cc and #004d99 (0x66 * 0.75 = 76.5 rounds to 0x4d), so the
    rounding is int(c * 0.75 + 0.5), not round(), which rounds half to even.
    """
    if not COLOR_RE.fullmatch(color):
        raise ValueError(f"not a #rrggbb color: {color!r}")
    channels = (int(color[i:i + 2], 16) for i in (1, 3, 5))
    return "#" + "".join(f"{int(c * 0.75 + 0.5):02x}" for c in channels)


def resolve(root: Path, raw: Any) -> Resolved | None:
    """The branding block with defaults applied (None without a block). Expects a block
    that passed check()."""
    if raw is None:
        return None
    if not isinstance(raw, dict):
        raise ValueError("branding must be a mapping")
    root = Path(root)
    text = raw.get("logo_text")
    text = str(text).strip() if text is not None else None
    mark = raw.get("wordmark")
    mark = str(mark).strip() if mark is not None else None
    logo = root / raw["logo"] if raw.get("logo") is not None else None
    alt = _one_line(raw.get("logo_alt")) or (f"Red Hat {text}" if text else mark or "")
    footer = raw.get("footer")
    if footer is None:
        footer = "none"
    elif isinstance(footer, dict):
        url = footer.get("url")
        footer = {"text": str(footer.get("text") or "").strip(), "url": str(url).strip() if url else None}
    favicon = root / raw["favicon"] if raw.get("favicon") is not None else None
    color = raw.get("color").lower() if isinstance(raw.get("color"), str) else None
    return Resolved(logo_text=text, logo=logo, alt=alt, footer=footer, favicon=favicon,
                    color=color, hover=hover_shade(color) if color else None,
                    wordmark=mark, theme="neutral" if raw.get("theme") == "neutral" else "stock")


# --- rendering -----------------------------------------------------------------

# Helvetica-Bold advance widths (Adobe Core 14 AFM, units per 1000 em) for printable
# ASCII 32 to 126; any other character counts as OTHER_WIDTH (the Helvetica-Bold average).
HELVETICA_BOLD = (
    278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,   # space to /
    556, 556, 556, 556, 556, 556, 556, 556, 556, 556,                                 # 0 to 9
    333, 333, 584, 584, 584, 611, 975,                                                # : to @
    722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833,                  # A to M
    722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611,                  # N to Z
    333, 278, 333, 584, 556, 333,                                                     # [ to `
    556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889,                  # a to m
    611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500,                  # n to z
    389, 280, 389, 584,                                                               # { to ~
)
OTHER_WIDTH = 611
FONT_SIZE = 82
TEXT_X = 308            # the text starts at translate(308 149) in the stock logo
TEXT_Y = 149
TEXT_FILL = "#fff"
LOGO_HEIGHT = 178.739   # the stock logo height (viewBox), scaled to 2rem by the header
TEXT_PAD = 24           # room after the text
MIN_VIEWBOX = 780       # the width of the stock logo-summit.svg, the narrowest stock logo
PLACEHOLDER_RE = re.compile(r"\{\{(\w+)\}\}")
XML_INVALID_RE = re.compile("[\x00-\x08\x0b\x0c\x0e-\x1f]")


def text_width(text: str) -> float:
    """Estimated rendered width of text in the logo (Helvetica-Bold at font size 82)."""
    units = sum(HELVETICA_BOLD[ord(c) - 32] if 32 <= ord(c) <= 126 else OTHER_WIDTH for c in text)
    return units * FONT_SIZE / 1000


def _xml(text: str) -> str:
    text = XML_INVALID_RE.sub("", text)
    return (text.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
            .replace('"', "&quot;").replace("'", "&apos;"))


def _html(text: str) -> str:
    """HTML-escaped text that Handlebars leaves alone ({ and } as entities)."""
    return html.escape(text, quote=True).replace("{", "&#123;").replace("}", "&#125;")


def _fill(template: str, values: dict[str, str]) -> str:
    """Fill every {{NAME}} placeholder in one pass, so filled-in text is never rescanned."""
    return PLACEHOLDER_RE.sub(lambda m: values.get(m.group(1), m.group(0)), template)


TEXT_ELEMENT_RE = re.compile(r"<text\b[\s\S]*?</text>")
# The attributes of the template's <text> element that the neutral outlines reproduce.
TEXT_ATTRS = (f'transform="translate({TEXT_X} {TEXT_Y})"', f'fill="{TEXT_FILL}"', f'font-size="{FONT_SIZE}"',
              f'font-weight="{LOGO_TEXT_WEIGHT}"')


def logo_svg(text: str, theme: str = "stock") -> str:
    """The Red Hat logo layout with text instead of "Demo Platform" (logo-text.svg.tmpl).

    Stock: the template's <text> element; textLength with lengthAdjust="spacingAndGlyphs"
    makes the text exactly as wide as the estimate, whatever font the viewer picks, so it
    never runs past the viewBox. Neutral: the <text> element is replaced by the text's
    outlines in Red Hat Display at the template's position, size, fill, and weight, and
    the viewBox width follows the real advance width.
    """
    template = (asset_dir() / "logo-text.svg.tmpl").read_text()
    if theme == "neutral":
        elements = TEXT_ELEMENT_RE.findall(template)
        if len(elements) != 1 or not all(attr in elements[0] for attr in TEXT_ATTRS):
            raise ValueError("logo-text.svg.tmpl: expected exactly one <text> element with "
                             + ", ".join(TEXT_ATTRS))
        paths, advance, _, right = _outline(text, LOGO_TEXT_WEIGHT)
        body = TEXT_ELEMENT_RE.sub(lambda _: _outline_notice() + _glyph_group(paths, TEXT_X, str(TEXT_Y)),
                                   template)
        width = max(MIN_VIEWBOX, math.ceil(TEXT_X + max(advance, right) + TEXT_PAD))
        return _fill(body, {"VIEWBOX_WIDTH": str(width)})
    width = text_width(text)
    return _fill(template, {
        "TEXT": _xml(text),
        "TEXT_LENGTH": f"{width:.2f}",
        "VIEWBOX_WIDTH": str(max(MIN_VIEWBOX, math.ceil(TEXT_X + width + TEXT_PAD))),
    })


_INSTANCES: dict[tuple[str, int, int], Any] = {}


def _display(weight: int) -> Any:
    """Red Hat Display instanced at weight (fontTools TTFont), cached per process and
    per font file (path and modification time), so a changed or relocated file is reread."""
    path = (fonts_dir() / "RedHatDisplay-VF.woff2").resolve()
    key = (str(path), path.stat().st_mtime_ns, weight)
    if key not in _INSTANCES:
        from fontTools.ttLib import TTFont
        from fontTools.varLib.instancer import instantiateVariableFont
        _INSTANCES[key] = instantiateVariableFont(TTFont(path), {"wght": weight}, inplace=False)
    return _INSTANCES[key]


def _num(value: float) -> str:
    """A path coordinate: at most two decimals, no trailing zeros, never -0."""
    text = f"{value:.2f}".rstrip("0").rstrip(".")
    return "0" if text in ("", "-0") else text


def _outline(text: str, weight: int, x0: float = 0.0) -> tuple[list[str], float, float, float]:
    """outline() starting at x0, plus the horizontal ink extent (left, right) of the drawn
    glyphs, which can reach past the advance width (a "j" starts left of x0)."""
    from fontTools.pens.boundsPen import BoundsPen
    from fontTools.pens.svgPathPen import SVGPathPen
    from fontTools.pens.transformPen import TransformPen
    font = _display(weight)
    cmap = font.getBestCmap()
    glyphs = font.getGlyphSet()
    scale = FONT_SIZE / font["head"].unitsPerEm
    x = x0
    left, right = x0, x0
    paths: list[str] = []
    for ch in text:
        name = cmap.get(ord(ch))
        if name is None:
            raise ValueError(f'no glyph for "{ch}" (U+{ord(ch):04X}) in Red Hat Display')
        transform = (scale, 0, 0, -scale, x, 0)
        pen = SVGPathPen(glyphs, ntos=_num)
        glyphs[name].draw(TransformPen(pen, transform))
        data = pen.getCommands()
        if data:
            paths.append(data)
            bounds = BoundsPen(glyphs)
            glyphs[name].draw(TransformPen(bounds, transform))
            if bounds.bounds:
                left, right = min(left, bounds.bounds[0]), max(right, bounds.bounds[2])
        x += glyphs[name].width * scale
    return paths, x - x0, left, right


def outline(text: str, weight: int) -> tuple[list[str], float]:
    """SVG path data for text in Red Hat Display at weight and font size 82: one string
    per glyph that draws something (spaces draw nothing), the baseline at y 0 and the
    first glyph at x 0; and the total advance width. No kerning (research R3)."""
    paths, advance, _, _ = _outline(text, weight)
    return paths, advance



def _glyph_group(paths: list[str], x: int, y: str) -> str:
    body = "".join(f'<path d="{d}"/>' for d in paths)
    return f'<g transform="translate({x} {y})" fill="{TEXT_FILL}">{body}</g>'


def _outline_notice() -> str:
    release = load_manifest().get("release", "")
    return (f"<!-- Text outlines rendered from Red Hat Display, release {release} "
            "(https://github.com/RedHatOfficial/RedHatFont), SIL Open Font License 1.1. -->")


def wordmark_svg(text: str) -> str:
    """The wordmark logo: text outlined in Red Hat Display Medium, white, without the Red
    Hat hat, at the stock logo height and text size, with the capitals centered
    vertically (the stock logo's baseline 149 belongs to its lower row; research R8). Ink
    that reaches left of the first glyph's origin shifts the text right, and the width
    covers both the advance and the ink, so no glyph is clipped."""
    paths, advance, left, right = _outline(text, WORDMARK_WEIGHT)
    shift = math.ceil(-left) if left < 0 else 0
    font = _display(WORDMARK_WEIGHT)
    cap = font["OS/2"].sCapHeight * FONT_SIZE / font["head"].unitsPerEm
    baseline = _num((LOGO_HEIGHT + cap) / 2)
    width = math.ceil(shift + max(advance, right))
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {width} {LOGO_HEIGHT}">'
            f"{_outline_notice()}{_glyph_group(paths, shift, baseline)}</svg>\n")


def theme_css(manifest: dict | None = None) -> str:
    """css/theme-neutral.css: @font-face rules for the vendored fonts (under the family
    names the bundle's font stacks already name) and the red navbar rule."""
    manifest = load_manifest() if manifest is None else manifest
    lines = ["/* Generated by tools/guide/branding.py from the guide.yaml branding theme; do not edit. */"]
    for entry in manifest["files"]:
        lo, hi = entry["weight"]
        lines.append(f'@font-face {{ font-family: "{entry["family"]}"; src: url("../{FONT_DIR}/{entry["file"]}") '
                     f'format("woff2"); font-weight: {lo} {hi}; font-style: {entry["style"]}; '
                     "font-display: swap; }")
    lines.append(".navbar { box-shadow: inset 0 -3px 0 #e00; }")
    return "\n".join(lines) + "\n"


def font_faces(manifest: dict | None = None) -> list[dict]:
    """The vendored faces for branding.json (the PDF printer reads them from there)."""
    manifest = load_manifest() if manifest is None else manifest
    return [{"family": e["family"], "file": e["file"], "weight": e["weight"], "style": e["style"]}
            for e in manifest["files"]]


def alt_helper(alt: str) -> str:
    """helpers/navbar-logo-alt.js returning alt; Handlebars escapes it in the attribute."""
    return ("'use strict'\n\n"
            "// Generated by tools/guide/branding.py from the guide.yaml branding block; do not edit.\n"
            f"module.exports = () => {json.dumps(alt)}\n")


def footer_partial(footer: str | dict) -> str:
    """partials/footer-content.hbs for footer none or {text, url} (rhdp keeps the bundle's)."""
    if footer == "none":
        return '<footer class="footer"></footer>\n'
    body = _html(footer["text"])
    if footer.get("url"):
        body = f'<a href="{_html(footer["url"])}" target="_blank" rel="noopener">{body}</a>'
    return f'<footer class="footer"><p class="guide-footer">{body}</p></footer>\n'


def head_meta(favicon_link: str, color: bool, extra_css: bool, neutral: bool = False) -> str:
    """partials/head-meta.hbs: the favicon link, then branding.css, theme-neutral.css, and
    extra.css after site.css."""
    lines = [favicon_link]
    if color:
        lines.append('<link rel="stylesheet" href="{{{uiRootPath}}}/css/branding.css">')
    if neutral:
        lines.append('<link rel="stylesheet" href="{{{uiRootPath}}}/css/theme-neutral.css">')
    if extra_css:
        lines.append('<link rel="stylesheet" href="{{{uiRootPath}}}/css/extra.css">')
    return "\n".join(lines) + "\n"


STOCK_FAVICON = '<link rel="icon" href="{{uiRootPath}}/img/favicon.ico" type="image/x-icon">'
# partials/head-styles.hbs for the neutral theme: the stock partial (contract rule
# head-styles-stock) without its three Google Fonts links, so a neutral site makes no
# third-party request and uses the vendored fonts only.
NEUTRAL_HEAD_STYLES = '    <link rel="stylesheet" href="{{{uiRootPath}}}/css/site.css">\n'


def load_contract(path: Path | None = None) -> dict:
    """The UI bundle contract (contract.yaml of asset_dir() unless path is given)."""
    data = yaml.safe_load(Path(path or asset_dir() / "contract.yaml").read_text())
    if not isinstance(data, dict):
        raise ValueError(f"{path or 'contract.yaml'}: not a mapping")
    return data


def accent_css(color: str, hover: str, contract: dict | None = None) -> str:
    """css/branding.css: the contract's accent selectors with color, hover selectors with hover.

    It loads after the bundle's site.css (head-meta.hbs), so the same selectors win
    without !important.
    """
    contract = load_contract() if contract is None else contract
    lines = ["/* Generated by tools/guide/branding.py from the guide.yaml branding color; do not edit. */"]
    for entry in contract.get("accent_selectors") or []:
        lines.append(f"{entry['selector']} {{ {entry['property']}: {color}; }}")
    for entry in contract.get("accent_hover_selectors") or []:
        lines.append(f"{entry['selector']} {{ {entry['property']}: {hover}; }}")
    for entry in contract.get("accent_restore_selectors") or []:
        lines.append(f"{entry['selector']} {{ {entry['property']}: {entry['value']}; }}")
    return "\n".join(lines) + "\n"


def favicon_links(suffix: str) -> tuple[str, str]:
    """The favicon link for head-meta.hbs and the head-icons.hbs content (stock forms)."""
    href = "/img/" + FAVICON_STEM.split("/", 1)[1] + suffix
    kind = FAVICON_TYPES[suffix]
    return (f'<link rel="icon" href="{{{{uiRootPath}}}}{href}" type="{kind}">',
            f'<link rel="icon" href="{{{{{{uiRootPath}}}}}}{href}" type="{kind}">\n')


def _write(ui: Path, rel: str, data: str | bytes) -> None:
    path = ui / rel
    path.parent.mkdir(parents=True, exist_ok=True)
    if isinstance(data, bytes):
        path.write_bytes(data)
    else:
        path.write_text(data)


def _swap(staged: Path, final: Path) -> None:
    """Move staged into final's place. POSIX rename cannot replace a non-empty directory,
    so the previous directory is renamed aside first and deleted afterwards."""
    old = final.with_name(f"{final.name}.old-{os.getpid()}")
    last: OSError | None = None
    for _ in range(5):
        shutil.rmtree(old, ignore_errors=True)
        if final.exists():
            try:
                final.rename(old)
            except FileNotFoundError:
                pass                      # another build moved it in the meantime
            except OSError as err:
                last = err
                continue
        try:
            staged.rename(final)
            break
        except OSError as err:
            last = err
            continue                      # another build swapped its result in; retry
    else:
        raise OSError(f"could not replace {final}: {last}") from last
    shutil.rmtree(old, ignore_errors=True)


def render(root: Path, view: str) -> Path:
    """Render build/branding/<view>/ (ui/ plus branding.json for a branded guide) and
    return the absolute path of its ui/ directory.

    Merge order: the files generated from the branding block, then every file of
    ui/supplemental/ on top (the author's file wins). The result is written to a
    temporary sibling and swapped in, so a concurrent build never reads half of it.
    """
    if view not in VIEWS:
        raise ValueError(f"view must be one of {', '.join(VIEWS)} (got {view!r})")
    root = Path(root).resolve()
    raw, fmt = load_block(root)
    problems, _ = check(root, raw, fmt, adapter_keys(root, fmt))
    if problems:
        raise ValueError("\n".join(f"{loc}: {msg}" for loc, msg in problems))
    brand = resolve(root, raw)
    overlay = overlay_files(root)

    base = root / "build" / "branding"
    final = base / view
    staged = base / f"{view}.tmp-{os.getpid()}"
    shutil.rmtree(staged, ignore_errors=True)
    try:
        _render_into(staged, final, root, brand, overlay)
    except BaseException:
        shutil.rmtree(staged, ignore_errors=True)    # never leave a half-written overlay behind
        raise
    return final / "ui"


def _render_into(staged: Path, final: Path, root: Path, brand: Resolved | None, overlay: list[str]) -> None:
    """Write the merged overlay of render() into staged, then swap it into final."""
    extra = EXTRA_CSS in overlay
    ui = staged / "ui"
    ui.mkdir(parents=True)

    generated: dict[str, str | bytes] = {}
    favicon_link = STOCK_FAVICON
    color = favicon = neutral = False
    manifest: dict | None = None
    if brand is not None:
        if brand.logo:
            generated[LOGO_FILE] = brand.logo.read_bytes()
        elif brand.wordmark:
            generated[LOGO_FILE] = wordmark_svg(brand.wordmark)
        else:
            generated[LOGO_FILE] = logo_svg(brand.logo_text, brand.theme)
        generated[ALT_HELPER] = alt_helper(brand.alt)
        if brand.footer != "rhdp":
            generated[FOOTER_PARTIAL] = footer_partial(brand.footer)
        if brand.favicon is not None:
            favicon = True
            suffix = brand.favicon.suffix.lower()
            generated[f"{FAVICON_STEM}{suffix}"] = brand.favicon.read_bytes()
            favicon_link, generated[HEAD_ICONS] = favicon_links(suffix)
        if brand.color is not None:
            color = True
            generated[BRANDING_CSS] = accent_css(brand.color, brand.hover)
        if brand.theme == "neutral":
            neutral = True
            manifest = load_manifest()
            generated[THEME_CSS] = theme_css(manifest)
            generated[HEAD_STYLES] = NEUTRAL_HEAD_STYLES
            for name in font_files(manifest):
                generated[f"{FONT_DIR}/{name}"] = (fonts_dir() / name).read_bytes()
    if color or favicon or extra or neutral:
        generated[HEAD_META] = head_meta(favicon_link, color, extra, neutral)
    for rel, data in generated.items():
        _write(ui, rel, data)
    for rel in overlay:
        target = ui / rel
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(root / OVERLAY / rel, target)

    if brand is not None:
        footer = brand.footer if isinstance(brand.footer, str) else "custom"
        meta = {"logo": str(final / "ui" / LOGO_FILE), "alt": brand.alt, "footer": footer,
                "theme": brand.theme, "fonts": str(final / "ui" / FONT_DIR) if neutral else None,
                "font_faces": font_faces(manifest) if neutral else [],
                "generated": sorted(generated), "shadowed": sorted(r for r in overlay if r in generated)}
        (staged / "branding.json").write_text(json.dumps(meta, indent=2) + "\n")
    _swap(staged, final)


# --- UI bundle contract --------------------------------------------------------------

CSS_COMMENT_RE = re.compile(r"/\*.*?\*/", re.S)
CSS_RULE_RE = re.compile(r"([^{}]+)\{([^{}]*)\}")
SITE_CSS = "css/site.css"
RULE_KEYS = {"id", "file", "exists", "contains", "matches", "equals"}


def _bundle_reader(bundle: Path):
    """A function that returns a bundle file's text (None when missing), for a bundle
    directory or a ui-bundle.zip."""
    bundle = Path(bundle)
    if bundle.is_dir():
        def read_dir(rel: str) -> str | None:
            path = bundle / rel
            return path.read_text(errors="replace") if path.is_file() else None
        return read_dir
    with zipfile.ZipFile(bundle) as zf:
        files = {name: zf.read(name) for name in zf.namelist() if not name.endswith("/")}

    def read_zip(rel: str) -> str | None:
        data = files.get(rel)
        return None if data is None else data.decode("utf-8", errors="replace")
    return read_zip


def _css_has(css: str, selector: str, value: str) -> bool:
    """True when css has a rule for selector whose declarations contain value."""
    want = " ".join(selector.split())
    pattern = re.compile(re.escape(value.lower()) + r"(?![0-9a-f])")
    for m in CSS_RULE_RE.finditer(CSS_COMMENT_RE.sub("", css)):
        selectors = [" ".join(s.split()) for s in m.group(1).split(",")]
        if want in selectors and pattern.search(m.group(2).lower()):
            return True
    return False


def check_bundle(bundle: Path, contract: Path | None = None) -> list[str]:
    """The contract lines a UI bundle (directory or zip) fails; empty when all hold.

    Every rule of contract.yaml, plus one rule per accent selector (accent:<selector>)
    and per restore selector (restore:<selector>): css/site.css must have a rule for the
    selector with the stock color (or the entry's value for accent_restore_selectors).
    """
    data = load_contract(contract)
    read = _bundle_reader(bundle)
    out: list[str] = []
    for rule in data.get("rules") or []:
        rid, rel = rule["id"], rule["file"]
        unknown = sorted(set(rule) - RULE_KEYS)
        if unknown or rule.get("exists", True) is not True:
            raise ValueError(f"contract rule {rid}: unsupported keys {unknown or ['exists']} "
                             "(every rule requires its file; exists may only be true)")
        text = read(rel)
        if text is None:
            out.append(f"contract: {rid}: {rel}: missing file")
            continue
        for needle in rule.get("contains") or []:
            if needle not in text:
                out.append(f'contract: {rid}: {rel}: missing "{needle}"')
        for regex in rule.get("matches") or []:
            if not re.search(regex, text):
                out.append(f"contract: {rid}: {rel}: no match for /{regex}/")
        if "equals" in rule and text.strip() != str(rule["equals"]).strip():
            out.append(f"contract: {rid}: {rel}: content differs from the recorded stock content")
    css = read(SITE_CSS)
    groups = (("accent_selectors", "accent", lambda e: data["accent_color"]),
              ("accent_hover_selectors", "accent", lambda e: data["accent_hover_color"]),
              ("accent_restore_selectors", "restore", lambda e: e["value"]))
    for key, prefix, value_of in groups:
        for entry in data.get(key) or []:
            rid, value = f"{prefix}:{entry['selector']}", str(value_of(entry))
            if css is None:
                out.append(f"contract: {rid}: {SITE_CSS}: missing file")
            elif not _css_has(css, entry["selector"], value):
                out.append(f"contract: {rid}: {SITE_CSS}: no rule for {entry['selector']} with {value}")
    return out


# --- CLI -------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Branding for Antora guides")
    sub = parser.add_subparsers(dest="cmd", required=True)
    v = sub.add_parser("validate", help="check the branding block and ui/supplemental/")
    v.add_argument("--root", type=Path, default=Path("."))
    r = sub.add_parser("render", help="render build/branding/<view>/ and print its ui/ path")
    r.add_argument("--view", choices=VIEWS, required=True)
    r.add_argument("--root", type=Path, default=Path("."))
    c = sub.add_parser("contract", help="check a UI bundle against the bundle contract")
    c.add_argument("--bundle", type=Path, required=True, help="ui-bundle.zip or an unpacked bundle directory")
    c.add_argument("--contract", type=Path, default=None, help="contract.yaml (default: the vendored one)")
    args = parser.parse_args(argv)
    if args.cmd == "contract":
        failures = check_bundle(args.bundle, args.contract)
        for line in failures:
            print(line)
        if failures:
            return 1
        print(f"contract: OK ({args.bundle})")
        return 0
    if args.cmd == "render":
        problems, warnings = check_guide(args.root)
        for line in problems + warnings:
            print(line, file=sys.stderr)
        if problems:
            return 1
        print(render(args.root, args.view))
        return 0
    problems, warnings = check_guide(args.root)
    for line in problems + warnings:
        print(line)
    if problems:
        return 1
    print("branding: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
