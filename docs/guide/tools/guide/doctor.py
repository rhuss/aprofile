#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Check a guide project's tool chain against tools/guide/adapter.yaml.

`requires` in the adapter descriptor maps a tool name to a version constraint
string, or to a mapping {version, command, hint}. Constraints: `*`,
`>=X[.Y[.Z]]`, `~X.Y.Z` (>=X.Y.Z, <X.(Y+1)), and `^X.Y` (>=X.Y, <(X+1)).
Known tools have a built-in check command and install hint; a mapping's
`command` and `hint` override them.

Usage: uv run tools/guide/doctor.py [--root DIR] [--adapter FILE]
--adapter reads the descriptor from FILE instead of <root>/tools/guide/adapter.yaml
(create checks the plugin's adapter before it scaffolds the guide project).
Prints one line per tool and exits 0 when everything is present, 1 otherwise.
"""
from __future__ import annotations

import argparse
import re
import shlex
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path

import yaml

VERSION_RE = re.compile(r"(\d+)\.(\d+)(?:\.(\d+))?")
CHROMIUM_JS = "const p=require('playwright');process.stdout.write(p.chromium.executablePath())"
TIMEOUT = 180


@dataclass
class Tool:
    command: list[str]
    hint: str


def builtin(name: str, root: Path) -> Tool | None:
    """Default check command and install hint for a known tool."""
    if name == "mkdocs":
        if (root / "requirements.txt").is_file():
            cmd = ["uvx", "--with-requirements", "requirements.txt", "mkdocs", "--version"]
        else:
            cmd = ["uvx", "mkdocs", "--version"]
        return Tool(cmd, "uv tool install mkdocs (or install uv: brew install uv)")
    table = {
        "node": (["node", "--version"], "brew install node"),
        "npm": (["npm", "--version"], "brew install node"),
        "uv": (["uv", "--version"], "brew install uv"),
        "git": (["git", "--version"], "brew install git"),
        "gh": (["gh", "--version"], "brew install gh"),
        "pandoc": (["pandoc", "--version"], "brew install pandoc"),
        "antora": (["npx", "--no-install", "antora", "--version"], "run npm install in the guide project"),
        "markserv": (["npx", "--no-install", "markserv", "--version"], "run npm install in the guide project"),
        "playwright-chromium": (["npx", "--no-install", "playwright", "--version"],
                                "npx playwright install chromium (after npm install)"),
    }
    if name not in table:
        return None
    cmd, hint = table[name]
    return Tool(list(cmd), hint)


def parse_version(text: str) -> tuple[int, int, int] | None:
    m = VERSION_RE.search(text or "")
    if not m:
        return None
    return int(m.group(1)), int(m.group(2)), int(m.group(3) or 0)


def _parts(text: str) -> tuple[int, int, int, int]:
    """Parse X[.Y[.Z]] into a padded tuple plus the number of given parts."""
    if not re.fullmatch(r"\d+(\.\d+){0,2}", text):
        raise ValueError(f"bad version {text!r}")
    nums = [int(n) for n in text.split(".")]
    given = len(nums)
    nums += [0] * (3 - given)
    return nums[0], nums[1], nums[2], given


def satisfies(version: tuple[int, int, int], constraint: str) -> bool:
    """True when version meets the constraint; ValueError for unknown syntax."""
    c = (constraint or "*").strip()
    if c == "*":
        return True
    if c.startswith(">="):
        x, y, z, _ = _parts(c[2:].strip())
        return version >= (x, y, z)
    if c.startswith("~"):
        x, y, z, given = _parts(c[1:].strip())
        upper = (x + 1, 0, 0) if given == 1 else (x, y + 1, 0)
        return (x, y, z) <= version < upper
    if c.startswith("^"):
        x, y, z, _ = _parts(c[1:].strip())
        return (x, y, z) <= version < (x + 1, 0, 0)
    raise ValueError(f"unknown version constraint {constraint!r}")


@dataclass
class Result:
    tool: str
    status: str  # ok | missing | old | error
    constraint: str = "*"
    version: str | None = None
    hint: str = ""
    message: str = ""

    def line(self) -> str:
        if self.status == "ok":
            return f"ok   {self.tool} {self.version or '?'} ({self.constraint})"
        if self.status == "missing":
            extra = f" ({self.message})" if self.message else ""
            return f"MISSING {self.tool}{extra}: {self.hint}"
        if self.status == "old":
            return f"OLD  {self.tool} {self.version} (need {self.constraint}): {self.hint}"
        return f"ERROR {self.tool}: {self.message}"


def _run(cmd: list[str], root: Path) -> tuple[bool, str]:
    cwd = root if Path(root).is_dir() else None
    try:
        proc = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=TIMEOUT)
    except (FileNotFoundError, PermissionError):
        return False, ""
    except subprocess.TimeoutExpired:
        return False, "timed out"
    return proc.returncode == 0, (proc.stdout or "") + (proc.stderr or "")


def _chromium_present(root: Path) -> bool:
    ok, out = _run(["node", "-e", CHROMIUM_JS], root)
    path = out.strip().splitlines()[-1].strip() if ok and out.strip() else ""
    return bool(path) and Path(path).is_file()


def check_tool(name: str, spec, root: Path) -> Result:
    constraint, command, hint = "*", None, None
    if isinstance(spec, dict):
        constraint = str(spec.get("version", "*"))
        command = spec.get("command")
        hint = spec.get("hint")
    elif spec is not None:
        constraint = str(spec)
    default = builtin(name, root)
    if command is None and default is None:
        return Result(name, "error", constraint, message="unknown tool without a command in adapter.yaml")
    cmd = shlex.split(command) if isinstance(command, str) else list(command or default.command)
    hint = hint or (default.hint if default else f"install {name}")
    try:
        satisfies((0, 0, 0), constraint)
    except ValueError as err:
        return Result(name, "error", constraint, message=f"{err} in adapter.yaml")

    if name == "playwright-chromium" and command is None and not _chromium_present(root):
        return Result(name, "missing", constraint, hint=hint, message="Chromium not installed")
    ok, out = _run(cmd, root)
    if not ok:
        return Result(name, "missing", constraint, hint=hint)
    version = parse_version(out)
    if version is None:
        if constraint.strip() in ("", "*"):
            return Result(name, "ok", constraint, "?", hint)
        return Result(name, "old", constraint, "unknown", hint, "could not read the version")
    vtext = ".".join(str(n) for n in version)
    if not satisfies(version, constraint):
        return Result(name, "old", constraint, vtext, hint)
    return Result(name, "ok", constraint, vtext, hint)


def check(root: Path, adapter: Path | None = None) -> list[Result]:
    root = Path(root)
    path = Path(adapter) if adapter else root / "tools" / "guide" / "adapter.yaml"
    if not path.is_file():
        return [Result("adapter.yaml", "error", message=f"{path} not found (is the toolkit installed?)")]
    try:
        data = yaml.safe_load(path.read_text()) or {}
    except yaml.YAMLError as err:
        return [Result("adapter.yaml", "error", message=f"invalid YAML: {err}")]
    requires = data.get("requires") or {}
    if not isinstance(requires, dict):
        return [Result("adapter.yaml", "error", message="requires must be a mapping of tool to constraint")]
    return [check_tool(str(name), spec, root) for name, spec in requires.items()]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Check the guide tool chain against tools/guide/adapter.yaml")
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--adapter", type=Path, help="adapter descriptor to read instead of tools/guide/adapter.yaml")
    args = parser.parse_args(argv)
    results = check(args.root, args.adapter)
    for r in results:
        print(r.line())
    failed = [r for r in results if r.status != "ok"]
    if failed:
        print(f"doctor: {len(failed)} of {len(results)} requirements not met")
        return 1
    print(f"doctor: all {len(results)} requirements met")
    return 0


if __name__ == "__main__":
    sys.exit(main())
