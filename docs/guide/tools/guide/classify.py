#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Read-only classifier, expectation matcher, redaction, and check extraction for validate.

A command is a query only if every pipeline segment matches the read-only allowlist and
none matches the explicit mutating list; everything else is mutating (default deny,
FR-038). The allowlist is read from <toolkit dir>/readonly.yaml, then
<toolkit dir>/../validate/readonly.yaml, and falls back to the identical built-in copy.

Usage:
  uv run tools/guide/classify.py classify CMD [--root DIR] [--explain]
  uv run tools/guide/classify.py extract [--root DIR] [--page PATH]...      (JSON list)
  uv run tools/guide/classify.py match MATCHER --stdout-file FILE --exit N  (exit 0 match, 1 mismatch, 2 bad matcher)
  uv run tools/guide/classify.py redact < output.txt
"""
from __future__ import annotations

import argparse
import copy
import fnmatch
import json
import re
import shlex
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Literal

import yaml

import guidecfg

Verdict = Literal["query", "mutating"]

_KUBE_VALUE_FLAGS = [
    "-n", "--namespace", "--context", "--kubeconfig", "--cluster", "--user", "-s", "--server",
    "--token", "--as", "--as-group", "--as-uid", "--request-timeout", "--certificate-authority",
    "--client-certificate", "--client-key", "--tls-server-name", "--cache-dir", "--loglevel",
    "-v", "--v", "-l", "--selector", "-o", "--output", "-c", "--container", "-f", "--filename",
    "-k", "--kustomize", "--field-selector", "--sort-by", "--template", "-L", "--label-columns",
    "--since", "--since-time", "--tail", "--chunk-size", "--limit-bytes", "--max-log-requests",
    "--subresource", "--api-group", "--raw",
]
_CONFIG_VIEW_DENY = ["--raw", "--flatten"]
# Flags that point oc or kubectl at another cluster or identity than the named connection.
_KUBE_CONNECTION_FLAGS = ["--kubeconfig", "--context", "--server", "-s", "--cluster", "--token", "--user"]

# Identical to guide/validate/readonly.yaml (a test asserts it).
DEFAULT_ALLOWLIST: dict[str, Any] = {
    "version": 1,
    "value_flags": {
        "oc": _KUBE_VALUE_FLAGS,
        "kubectl": _KUBE_VALUE_FLAGS,
        "curl": [
            "-A", "-b", "-c", "-C", "-d", "-D", "-e", "-E", "-F", "-H", "-K", "-m", "-o", "-P", "-Q",
            "-r", "-T", "-t", "-u", "-U", "-w", "-x", "-X", "-y", "-Y", "-z",
            "--user-agent", "--cookie", "--cookie-jar", "--data", "--data-ascii", "--data-binary",
            "--data-raw", "--data-urlencode", "--dump-header", "--referer", "--cert", "--cacert",
            "--key", "--form", "--form-string", "--header", "--config", "--max-time",
            "--connect-timeout", "--output", "--range", "--upload-file", "--user", "--write-out",
            "--proxy", "--request", "--resolve", "--retry", "--json", "--url", "--oauth2-bearer",
        ],
    },
    "queries": [
        "oc get", "oc describe", "oc logs", "oc explain", "oc api-resources", "oc api-versions",
        "oc version", "oc whoami", "oc auth can-i", "oc adm top",
        {"pattern": "oc config view --minify", "deny_flags": _CONFIG_VIEW_DENY},
        "oc config current-context",
        "kubectl get", "kubectl describe", "kubectl logs", "kubectl explain", "kubectl api-resources",
        "kubectl api-versions", "kubectl version", "kubectl whoami", "kubectl auth can-i",
        "kubectl adm top",
        {"pattern": "kubectl config view --minify", "deny_flags": _CONFIG_VIEW_DENY},
        "kubectl config current-context",
        {"pattern": "curl", "methods": ["GET", "HEAD"],
         "deny_flags": ["-d", "--data*", "--json", "-F", "--form*", "-T", "--upload-file"]},
        "openshell status", "openshell version", "openshell whoami", "openshell list",
        "openshell get", "openshell * list", "openshell * get",
        "grep",
        {"pattern": "rg", "deny_flags": ["--pre*"]},
        "jq",
        {"pattern": "yq", "deny_flags": ["-i", "--inplace*"]},
        "head", "tail", "wc",
        {"pattern": "sort", "deny_flags": ["-o", "--output*"]},
        {"pattern": "uniq", "max_args": 1},
        "cut",
        {"pattern": "awk", "deny_flags": ["-f", "--file*"], "deny_args": [r"system\(", "getline", r"\|", "print[^;}]*>"]},
    ],
    "mutating": [
        "oc debug", "oc exec", "oc rsh", "oc port-forward", "oc attach", "oc cp", "oc rsync",
        "kubectl debug", "kubectl exec", "kubectl port-forward", "kubectl attach", "kubectl cp",
        "make", "tee", "sudo", "xargs", "eval", "sh", "bash", "zsh",
    ],
    "redirection": {"allowed_targets": ["/dev/null"]},
    "deny_flags": {
        "oc": _KUBE_CONNECTION_FLAGS,
        "kubectl": _KUBE_CONNECTION_FLAGS,
        "curl": ["-o", "--output", "-O", "--remote-name*", "-K", "--config", "-c", "--cookie-jar",
                 "-D", "--dump-header"],
    },
}

SEPARATORS = {"|", "||", "&&", ";", "|&"}
ENV_ASSIGN_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*=")
METHOD_FLAGS = ("-X", "--request")


# --- allowlist ----------------------------------------------------------------


@dataclass(frozen=True)
class Rule:
    """One allowlist or mutating-list entry."""

    source: str
    words: tuple[str, ...]
    flags: tuple[str, ...] = ()
    deny_flags: tuple[str, ...] = ()
    deny_args: tuple[str, ...] = ()
    methods: tuple[str, ...] | None = None
    max_args: int | None = None

    @classmethod
    def from_entry(cls, entry: Any) -> "Rule":
        if isinstance(entry, str):
            entry = {"pattern": entry}
        if not isinstance(entry, dict) or not isinstance(entry.get("pattern"), str):
            raise ValueError(f"bad allowlist entry {entry!r} (expected a pattern string or mapping)")
        unknown = set(entry) - {"pattern", "deny_flags", "deny_args", "methods", "max_args"}
        if unknown:
            raise ValueError(f"bad allowlist entry {entry!r}: unknown keys {sorted(unknown)}")
        parts = entry["pattern"].split()
        if not parts:
            raise ValueError("empty allowlist pattern")
        for rx in entry.get("deny_args") or []:
            re.compile(rx)
        methods = entry.get("methods")
        return cls(
            source=entry["pattern"],
            words=tuple(p for p in parts if not p.startswith("-")),
            flags=tuple(p for p in parts if p.startswith("-")),
            deny_flags=tuple(entry.get("deny_flags") or ()),
            deny_args=tuple(entry.get("deny_args") or ()),
            methods=tuple(m.upper() for m in methods) if methods else None,
            max_args=entry.get("max_args"),
        )


@dataclass
class Allowlist:
    queries: list[Rule]
    mutating: list[Rule]
    value_flags: dict[str, frozenset[str]] = field(default_factory=dict)
    allowed_targets: frozenset[str] = frozenset({"/dev/null"})
    tool_deny_flags: dict[str, tuple[str, ...]] = field(default_factory=dict)

    @classmethod
    def from_data(cls, data: dict) -> "Allowlist":
        if not isinstance(data, dict):
            raise ValueError("allowlist must be a mapping")
        return cls(
            queries=[Rule.from_entry(e) for e in data.get("queries") or []],
            mutating=[Rule.from_entry(e) for e in data.get("mutating") or []],
            value_flags={tool: frozenset(flags) for tool, flags in (data.get("value_flags") or {}).items()},
            allowed_targets=frozenset((data.get("redirection") or {}).get("allowed_targets") or ["/dev/null"]),
            tool_deny_flags={tool: tuple(flags) for tool, flags in (data.get("deny_flags") or {}).items()},
        )


def allowlist_path(toolkit_dir: Path) -> Path | None:
    """The allowlist file next to the toolkit, or in the plugin's validate/ directory."""
    for candidate in (Path(toolkit_dir) / "readonly.yaml", Path(toolkit_dir).parent / "validate" / "readonly.yaml"):
        if candidate.is_file():
            return candidate
    return None


def load_allowlist(guide: Any = None, path: Path | None = None) -> Allowlist:
    """Default allowlist plus the guide's validate.extra_queries (additive only).

    `guide` is a guidecfg.Guide, a list of extra query patterns, or None.
    """
    if path is None:
        path = allowlist_path(Path(__file__).resolve().parent)
    data = yaml.safe_load(Path(path).read_text()) if path else copy.deepcopy(DEFAULT_ALLOWLIST)
    allow = Allowlist.from_data(data)
    extras = guide if isinstance(guide, list) else (getattr(guide, "extra_queries", None) or [])
    allow.queries.extend(Rule.from_entry(e) for e in extras)
    return allow


# --- classification -----------------------------------------------------------


@dataclass
class Segment:
    text: str
    verdict: Verdict
    rule: Rule | None = None
    reason: str = ""


def _strip_comment(line: str) -> str:
    """Drop a shell comment: `#` at the start of a word, outside quotes."""
    quote = None
    prev = " "
    i = 0
    while i < len(line):
        ch = line[i]
        if quote:
            if ch == "\\" and quote == '"':
                i += 2
                prev = "x"
                continue
            if ch == quote:
                quote = None
        elif ch == "\\":
            i += 2
            prev = "x"
            continue
        elif ch in "'\"":
            quote = ch
        elif ch == "#" and (prev.isspace() or prev in ";|&()"):
            return line[:i]
        prev = ch
        i += 1
    return line


def _tokens(line: str) -> list[str]:
    lexer = shlex.shlex(line, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    lexer.commenters = ""
    return list(lexer)


def _is_op(token: str) -> bool:
    return bool(token) and all(c in "();<>|&" for c in token)


def parse_args(tool: str, args: list[str], value_flags: frozenset[str]) -> tuple[list[str], list[tuple[str, str | None]]]:
    """Split arguments into positional words and (flag, value) pairs."""
    positional: list[str] = []
    flags: list[tuple[str, str | None]] = []
    i = 0
    while i < len(args):
        arg = args[i]
        if arg == "--":
            positional.extend(args[i + 1:])
            break
        if arg.startswith("--") and len(arg) > 2:
            name, eq, value = arg.partition("=")
            if eq:
                flags.append((name, value))
            elif name in value_flags and i + 1 < len(args) and not args[i + 1].startswith("-"):
                flags.append((name, args[i + 1]))
                i += 1
            else:
                flags.append((name, None))
        elif arg.startswith("-") and len(arg) > 1:
            body = arg[1:]
            for j, ch in enumerate(body):
                name = f"-{ch}"
                if name in value_flags:
                    rest = body[j + 1:]
                    if rest:
                        flags.append((name, rest[1:] if rest.startswith("=") else rest))
                    elif i + 1 < len(args) and not args[i + 1].startswith("-"):
                        flags.append((name, args[i + 1]))
                        i += 1
                    else:
                        flags.append((name, None))
                    break
                flags.append((name, None))
        else:
            positional.append(arg)
        i += 1
    return positional, flags


def _prefix_matches(pattern: tuple[str, ...], positional: list[str]) -> bool:
    if len(positional) < len(pattern):
        return False
    return all(p == "*" or p == w for p, w in zip(pattern, positional))


def _rule_matches(rule: Rule, tool: str, args: list[str], allow: Allowlist, mutating: bool = False) -> bool:
    if tool != rule.words[0]:
        return False
    positional, flags = parse_args(tool, args, allow.value_flags.get(tool, frozenset()))
    if not _prefix_matches(rule.words[1:], positional):
        return False
    if mutating:
        return True
    names = {name for name, _ in flags}
    if any(f not in names for f in rule.flags):
        return False
    if any(fnmatch.fnmatchcase(name, pat) for name in names for pat in rule.deny_flags):
        return False
    # Tool-wide deny flags hold for every query of the tool, extra_queries included.
    # A value in the allowed redirect targets (/dev/null) writes and reads nothing.
    if any(fnmatch.fnmatchcase(name, pat) and value not in allow.allowed_targets
           for name, value in flags for pat in allow.tool_deny_flags.get(tool, ())):
        return False
    if rule.max_args is not None and len(positional) - (len(rule.words) - 1) > rule.max_args:
        return False
    if any(re.search(rx, a) for a in args for rx in rule.deny_args):
        return False
    if rule.methods is not None:
        for name, value in flags:
            if name in METHOD_FLAGS and (value is None or value.upper() not in rule.methods):
                return False
    return True


def _classify_segment(tokens: list[str], allow: Allowlist) -> Segment:
    text = shlex.join(tokens)
    words: list[str] = []
    i = 0
    while i < len(tokens):
        tok = tokens[i]
        if _is_op(tok):
            if "(" in tok or ")" in tok:
                return Segment(text, "mutating", reason="subshell or process substitution")
            target = tokens[i + 1] if i + 1 < len(tokens) else None
            if words and words[-1].isdigit() and i > 0 and tokens[i - 1] == words[-1]:
                words.pop()  # file descriptor number such as the 2 in 2>/dev/null
            if ">" in tok:
                if target is None:
                    return Segment(text, "mutating", reason=f"redirection {tok} without a target")
                fd_dup = tok.endswith(">&") and (target.isdigit() or target == "-")
                if not fd_dup and target not in allow.allowed_targets:
                    return Segment(text, "mutating", reason=f"output redirection to {target}")
                i += 2
                continue
            if tok in ("<", "<<<"):
                i += 2
                continue
            return Segment(text, "mutating", reason=f"shell operator {tok!r}")
        words.append(tok)
        i += 1
    while words and ENV_ASSIGN_RE.match(words[0]):
        words.pop(0)
    if not words:
        return Segment(text, "mutating", reason="no command")
    tool = Path(words[0]).name
    args = words[1:]
    for rule in allow.mutating:
        if _rule_matches(rule, tool, args, allow, mutating=True):
            return Segment(text, "mutating", rule=rule, reason=f"explicitly mutating ({rule.source})")
    for rule in allow.queries:
        if _rule_matches(rule, tool, args, allow):
            return Segment(text, "query", rule=rule, reason=f"allowlisted ({rule.source})")
    return Segment(text, "mutating", reason="not on the read-only allowlist")


def explain(cmd: str, allow: Allowlist) -> list[Segment]:
    """Per-segment verdicts with the matching rule or the reason for mutating."""
    if not cmd or not cmd.strip():
        return [Segment(cmd, "mutating", reason="empty command")]
    if "`" in cmd or "$(" in cmd:
        return [Segment(cmd, "mutating", reason="command substitution")]
    joined = re.sub(r"\\\n", " ", cmd)
    segments: list[Segment] = []
    for line in joined.splitlines():
        line = _strip_comment(line).strip()
        if not line:
            continue
        try:
            tokens = _tokens(line)
        except ValueError as err:
            return [Segment(line, "mutating", reason=f"cannot parse: {err}")]
        current: list[str] = []
        for tok in tokens + [";"]:
            if tok in SEPARATORS:
                if current:
                    segments.append(_classify_segment(current, allow))
                current = []
            elif tok == "&" or (tok.startswith("&") and _is_op(tok) and ">" not in tok):
                segments.append(Segment(line, "mutating", reason="background or unknown operator"))
                current = []
            else:
                current.append(tok)
    return segments or [Segment(cmd, "mutating", reason="empty command")]


def classify(cmd: str, allow: Allowlist) -> Verdict:
    return "query" if all(s.verdict == "query" for s in explain(cmd, allow)) else "mutating"


# --- expectations -------------------------------------------------------------


class MatcherError(ValueError):
    """An expectation does not follow the matcher grammar."""


@dataclass
class Term:
    kind: str  # exit | contains | not-contains | matches | lines
    value: Any
    op: str = ""


_LINES_RE = re.compile(r"lines\s*(>=|<=|==)\s*(\d+)")
_EXIT_RE = re.compile(r"exit\s+(-?\d+)")


def _quoted(text: str, pos: int) -> tuple[str, int]:
    if pos >= len(text) or text[pos] not in "\"'":
        raise MatcherError(f"expected a quoted string at position {pos} in {text!r}")
    quote = text[pos]
    out = []
    i = pos + 1
    while i < len(text):
        ch = text[i]
        if ch == "\\" and i + 1 < len(text):
            out.append(text[i + 1])
            i += 2
            continue
        if ch == quote:
            return "".join(out), i + 1
        out.append(ch)
        i += 1
    raise MatcherError(f"unterminated string in {text!r}")


def _regex(text: str, pos: int) -> tuple[re.Pattern, int]:
    if pos >= len(text) or text[pos] != "/":
        raise MatcherError(f"expected /regex/ at position {pos} in {text!r}")
    i = pos + 1
    out = []
    while i < len(text):
        ch = text[i]
        if ch == "\\" and i + 1 < len(text):
            out.append("/" if text[i + 1] == "/" else text[i:i + 2])
            i += 2
            continue
        if ch == "/":
            try:
                return re.compile("".join(out), re.M), i + 1
            except re.error as err:
                raise MatcherError(f"invalid regex in {text!r}: {err}") from err
        out.append(ch)
        i += 1
    raise MatcherError(f"unterminated /regex/ in {text!r}")


def parse_matcher(text: str) -> list[Term]:
    """Parse an expectation; raise MatcherError on bad syntax."""
    terms: list[Term] = []
    pos = 0
    n = len(text)

    def skip_ws(p: int) -> int:
        while p < n and text[p].isspace():
            p += 1
        return p

    pos = skip_ws(pos)
    if pos >= n:
        raise MatcherError("empty expectation")
    while True:
        rest = text[pos:]
        if rest.startswith("not-contains"):
            value, pos = _quoted(text, skip_ws(pos + len("not-contains")))
            terms.append(Term("not-contains", value))
        elif rest.startswith("contains"):
            value, pos = _quoted(text, skip_ws(pos + len("contains")))
            terms.append(Term("contains", value))
        elif rest.startswith("matches"):
            rx, pos = _regex(text, skip_ws(pos + len("matches")))
            terms.append(Term("matches", rx))
        elif m := _LINES_RE.match(rest):
            terms.append(Term("lines", int(m.group(2)), m.group(1)))
            pos += m.end()
        elif m := _EXIT_RE.match(rest):
            terms.append(Term("exit", int(m.group(1))))
            pos += m.end()
        else:
            raise MatcherError(f"unknown matcher at {rest!r} (expected exit, contains, not-contains, matches, or lines)")
        pos = skip_ws(pos)
        if pos >= n:
            return terms
        if not (text.startswith("and", pos) and pos + 3 < n and text[pos + 3].isspace()):
            raise MatcherError(f"expected 'and' at {text[pos:]!r}")
        pos = skip_ws(pos + 3)
        if pos >= n:
            raise MatcherError("dangling 'and'")


def match(matcher: str, stdout: str, exit_code: int) -> bool:
    """Evaluate an expectation against a command's stdout and exit status.

    `lines` counts non-blank lines; `matches` uses re.search with multiline anchors.
    """
    for term in parse_matcher(matcher):
        if term.kind == "exit":
            ok = exit_code == term.value
        elif term.kind == "contains":
            ok = term.value in stdout
        elif term.kind == "not-contains":
            ok = term.value not in stdout
        elif term.kind == "matches":
            ok = term.value.search(stdout) is not None
        else:
            count = sum(1 for line in stdout.splitlines() if line.strip())
            ok = {">=": count >= term.value, "<=": count <= term.value, "==": count == term.value}[term.op]
        if not ok:
            return False
    return True


# --- redaction ----------------------------------------------------------------

_REDACTED = "[REDACTED]"
_SECRET_KEY = (r"[\w.-]*(?:token|secret|password|passwd|apikey|api[-_]key|access[-_]key|private[-_]key"
               r"|credential)[\w.-]*|client-key-data|client-certificate-data|certificate-authority-data")
_REDACTIONS: list[tuple[re.Pattern, str]] = [
    (re.compile(r"-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----", re.S),
     "-----BEGIN PRIVATE KEY----- " + _REDACTED + " -----END PRIVATE KEY-----"),
    (re.compile(r"(?i)(authorization\s*:\s*)[^'\"\r\n]+"), r"\1" + _REDACTED),
    (re.compile(r"(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+"), r"\1" + _REDACTED),
    (re.compile(r"(--(?:token|password|client-key|oauth2-bearer)(?:=|\s+))\S+"), r"\1" + _REDACTED),
    (re.compile(r"(?i)([\"']?(?:" + _SECRET_KEY + r")[\"']?\s*[:=]\s*[\"']?)"
                r"(?!\[REDACTED\])[^\s\"',}]+"), r"\1" + _REDACTED),
    (re.compile(r"sha256~[A-Za-z0-9_-]+"), "sha256~" + _REDACTED),
    (re.compile(r"\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*"), _REDACTED),
    (re.compile(r"\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,})"), _REDACTED),
    (re.compile(r"\bxox[abprs]-[A-Za-z0-9-]{10,}"), _REDACTED),
    (re.compile(r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b"), _REDACTED),
]
_YAML_DATA = re.compile(r"^(\s*)(data|stringData|binaryData):\s*$")
_YAML_ENTRY = re.compile(r"^(\s*[^\s:#][^:]*:\s*)(\S.*)$")
_JSON_DATA = re.compile(r'"(?:data|stringData|binaryData)"\s*:\s*\{[^{}]*\}', re.S)
_JSON_VALUE = re.compile(r'(:\s*)"(?!\[REDACTED\])[^"]*"')


def _redact_secret_data(text: str) -> str:
    """Values under Secret data/stringData/binaryData (YAML and JSON output) are always redacted."""
    text = _JSON_DATA.sub(lambda m: _JSON_VALUE.sub(r'\1"' + _REDACTED + '"', m.group(0)), text)
    out: list[str] = []
    indent: int | None = None
    for line in text.split("\n"):
        if indent is not None:
            stripped = line.strip()
            width = len(line) - len(line.lstrip())
            if stripped and width <= indent:
                indent = None
            else:
                m = _YAML_ENTRY.match(line)
                if m and stripped:
                    line = m.group(1) + _REDACTED
        m = _YAML_DATA.match(line)
        if m:
            indent = len(m.group(1))
        out.append(line)
    return "\n".join(out)


def redact(text: str) -> str:
    """Remove credentials and tokens from command output before it is written anywhere (FR-039a)."""
    text = _redact_secret_data(text)
    for pattern, repl in _REDACTIONS:
        text = pattern.sub(repl, text)
    return text


# --- extraction ---------------------------------------------------------------


@dataclass
class CheckStep:
    page: Path
    line: int
    commands: list[str]
    expectation: str | None
    text: str = ""


_ADOC_LISTING = re.compile(r"^(-{4,}|\.{4,})$")
_ADOC_SKIP_BLOCK = re.compile(r"^(-{4,}|\.{4,}|\+{4,}|/{4,})$")
_ADOC_EXPECT = re.compile(r"^//\s*expect:\s*(.+?)\s*$")
_MD_FENCE = re.compile(r"^(\s*)(`{3,}|~{3,})(.*)$")
_MD_EXPECT = re.compile(r"^<!--\s*expect:\s*(.+?)\s*-->$")


def _commands(body: list[str]) -> list[str]:
    lines = [line.rstrip() for line in body]
    prompted = any(line.lstrip().startswith("$ ") for line in lines)
    cmds: list[str] = []
    current: str | None = None
    for line in lines:
        stripped = line.strip()
        if current is not None:
            if stripped.endswith("\\"):
                current += " " + stripped[:-1].rstrip()
                continue
            cmds.append((current + " " + stripped).strip())
            current = None
            continue
        if prompted:
            if not stripped.startswith("$ "):
                continue
            stripped = stripped[2:].strip()
        if not stripped or stripped.startswith("#"):
            continue
        if stripped.endswith("\\"):
            current = stripped[:-1].rstrip()
            continue
        cmds.append(stripped)
    if current:
        cmds.append(current.strip())
    return cmds


def _expectation(lines: list[str], start: int, pattern: re.Pattern) -> str | None:
    j = start
    while j < len(lines) and not lines[j].strip():
        j += 1
    if j < len(lines):
        m = pattern.match(lines[j].strip())
        if m:
            return m.group(1)
    return None


def _extract_line(page: Path, lines: list[str]) -> list[CheckStep]:
    steps: list[CheckStep] = []
    i = 0
    n = len(lines)
    while i < n:
        s = lines[i].strip()
        if _ADOC_SKIP_BLOCK.match(s):
            j = i + 1
            while j < n and lines[j].strip() != s:
                j += 1
            i = j + 1
            continue
        if not s.startswith("Check:"):
            i += 1
            continue
        step = CheckStep(page, i + 1, [], None, s[len("Check:"):].strip())
        steps.append(step)
        j = i + 1
        while j < n and (not lines[j].strip() or lines[j].strip() == "+"
                         or re.match(r"^\[.*\]$", lines[j].strip())
                         or re.match(r"^\.[^\s.]", lines[j].strip())):
            j += 1
        if j < n and _ADOC_LISTING.match(lines[j].strip()):
            delim = lines[j].strip()
            k = j + 1
            body = []
            while k < n and lines[k].strip() != delim:
                body.append(lines[k])
                k += 1
            step.commands = _commands(body)
            step.expectation = _expectation(lines, k + 1, _ADOC_EXPECT)
            i = k + 1
            continue
        i += 1
    return steps


def _extract_html(page: Path, lines: list[str]) -> list[CheckStep]:
    steps: list[CheckStep] = []
    i = 0
    n = len(lines)

    def fence_end(start: int, marker: str) -> int:
        k = start
        while k < n:
            m = _MD_FENCE.match(lines[k])
            if m and m.group(2)[0] == marker[0] and len(m.group(2)) >= len(marker) and not m.group(3).strip():
                return k
            k += 1
        return n

    while i < n:
        fence = _MD_FENCE.match(lines[i])
        if fence:
            i = fence_end(i + 1, fence.group(2)) + 1
            continue
        s = lines[i].strip().lstrip("> ").strip()
        if not s.startswith("Check:"):
            i += 1
            continue
        step = CheckStep(page, i + 1, [], None, s[len("Check:"):].strip())
        steps.append(step)
        j = i + 1
        while j < n and not lines[j].strip():
            j += 1
        fence = _MD_FENCE.match(lines[j]) if j < n else None
        if fence:
            indent = len(fence.group(1))
            k = fence_end(j + 1, fence.group(2))
            body = [line[indent:] if line[:indent].isspace() else line.lstrip() for line in lines[j + 1:k]]
            step.commands = _commands(body)
            step.expectation = _expectation(lines, k + 1, _MD_EXPECT)
            i = k + 1
            continue
        i += 1
    return steps


def extract(pages: list[Path], style: Literal["line", "html"]) -> list[CheckStep]:
    """Every `Check:` step with its listing's commands and its expectation (None when absent)."""
    steps: list[CheckStep] = []
    for page in pages:
        lines = Path(page).read_text(errors="replace").splitlines()
        steps.extend(_extract_line(Path(page), lines) if style == "line" else _extract_html(Path(page), lines))
    return steps


# --- CLI ----------------------------------------------------------------------


def _guide_or_none(root: Path) -> guidecfg.Guide | None:
    try:
        return guidecfg.load_guide(root) if (Path(root) / "guide.yaml").is_file() else None
    except guidecfg.ConfigError as err:
        print(err, file=sys.stderr)
        return None


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Read-only classifier and expectation matcher for /guide:validate")
    sub = parser.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("classify")
    c.add_argument("command")
    c.add_argument("--root", type=Path)
    c.add_argument("--explain", action="store_true")
    e = sub.add_parser("extract")
    e.add_argument("--root", type=Path, default=Path("."))
    e.add_argument("--page", type=Path, action="append", default=[])
    m = sub.add_parser("match")
    m.add_argument("matcher")
    m.add_argument("--stdout-file", type=Path, required=True)
    m.add_argument("--exit", type=int, required=True, dest="exit_code")
    sub.add_parser("redact")
    args = parser.parse_args(argv)

    if args.cmd == "classify":
        allow = load_allowlist(_guide_or_none(args.root) if args.root else None)
        segments = explain(args.command, allow)
        print("query" if all(s.verdict == "query" for s in segments) else "mutating")
        if args.explain:
            for s in segments:
                print(f"  {s.verdict:8} {s.text}  ({s.reason})")
        return 0
    if args.cmd == "match":
        try:
            ok = match(args.matcher, args.stdout_file.read_text(errors="replace"), args.exit_code)
        except MatcherError as err:
            print(f"bad matcher: {err}", file=sys.stderr)
            return 2
        print("match" if ok else "mismatch")
        return 0 if ok else 1
    if args.cmd == "redact":
        sys.stdout.write(redact(sys.stdin.read()))
        return 0

    root = args.root
    adapter = guidecfg.load_adapter(root)
    style = "line" if adapter.comment == "line" else "html"
    pages = [p if p.is_absolute() else root / p for p in args.page] or guidecfg.pages(root, adapter, "all")
    allow = load_allowlist(_guide_or_none(root))
    out = []
    for step in extract(pages, style):
        verdicts = [classify(cmd, allow) for cmd in step.commands]
        try:
            rel = step.page.resolve().relative_to(Path(root).resolve()).as_posix()
        except ValueError:
            rel = str(step.page)
        out.append({
            "page": rel,
            "line": step.line,
            "text": step.text,
            "commands": step.commands,
            "classifications": verdicts,
            "classification": "query" if verdicts and all(v == "query" for v in verdicts) else "mutating",
            "expectation": step.expectation,
            "evaluable": step.expectation is not None and bool(step.commands),
            "internal": guidecfg.is_internal(step.page, root, adapter),
        })
    print(json.dumps(out, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
