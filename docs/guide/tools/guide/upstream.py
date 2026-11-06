#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Upstream findings: re-verify, draft, sanitize, dedupe, and look for existing reports.

Findings live in findings/upstream.yaml (see data-model.md). Nothing here posts
anything: /guide:upstream shows every draft in full and files only approved ones.

Usage:
  uv run tools/guide/upstream.py reverify [--root DIR] [--no-fetch] (--all | ID...)
  uv run tools/guide/upstream.py draft [--root DIR] [--run-id ID] [--offline] ID...
  uv run tools/guide/upstream.py sanitize [--root DIR] [--offline] FILE...
  uv run tools/guide/upstream.py dedupe [--root DIR]
  uv run tools/guide/upstream.py existing [--root DIR] ID...

A draft file starts with `# <issue title>`; everything after the first blank line
is the issue body. All git calls run as `git -C <cache path>`; nothing changes the
working directory.
"""
from __future__ import annotations

import argparse
import functools
import json
import os
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

import yaml

import classify
import fetch
import guidecfg
import runs

HEADER = "# Upstream findings collected by cc-guide runs; file them with /guide:upstream.\n"
ACTIVE = ("collected", "reverified", "drafted", "approved", "needs-review")
# Only findings not yet drafted can be merged into another (data-model.md transitions).
MERGEABLE = ("collected", "reverified", "needs-review")
TERMINAL = ("filed", "merged", "dropped", "rejected")
DRAFTABLE = ("reverified", "drafted")
NOTE_PREFIX = "re-verification"
GITHUB_URL_RE = re.compile(r"https?://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)")
GITHUB_NON_REPO = {"orgs", "settings", "features", "about", "topics", "marketplace", "sponsors"}
REF_RE = re.compile(r"^([^:\s]+)(?::(\d+)(?:-(\d+))?)?$")
STOPWORDS = {"the", "and", "for", "with", "while", "that", "this", "from", "into", "uses", "use",
             "are", "but", "not", "its", "has", "have", "was", "were", "when", "which", "does"}


class UpstreamError(Exception):
    """A finding cannot be processed (unknown id, unknown source, missing cache)."""


@dataclass
class Problem:
    kind: str  # term, pattern, internal-link, private-repo, own-repo
    match: str
    message: str
    severity: str = "error"  # error blocks the draft; warning needs the author's look

    def __str__(self) -> str:
        return f"{self.severity}: [{self.kind}] {self.message}"


@dataclass
class Merge:
    kept: str
    merged: str


# --- findings file --------------------------------------------------------------


def _path(root: Path) -> Path:
    return Path(root) / "findings" / "upstream.yaml"


def _load(root: Path) -> tuple[str, list[dict]]:
    path = _path(root)
    if not path.is_file():
        return HEADER, []
    text = path.read_text()
    header = "".join(line for line in text.splitlines(keepends=True)[:5] if line.startswith("#")) or HEADER
    return header, guidecfg.load_findings(root)


def _save(root: Path, header: str, findings: list[dict]) -> None:
    path = _path(root)
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_name(".upstream.yaml.tmp")
    tmp.write_text(header + yaml.safe_dump({"findings": findings}, sort_keys=False, allow_unicode=True))
    os.replace(tmp, path)


def _find(findings: list[dict], finding_id: str) -> dict:
    for f in findings:
        if f.get("id") == finding_id:
            return f
    raise UpstreamError(f"unknown finding {finding_id!r} (not in findings/upstream.yaml)")


def _source(root: Path, finding: dict) -> guidecfg.Source:
    source = guidecfg.load_baseline(root).source(finding.get("source"))
    if source is None:
        raise UpstreamError(f"{finding.get('id')}: unknown source {finding.get('source')!r}")
    return source


def _split_ref(ref: str) -> tuple[str, tuple[int, int] | None]:
    m = REF_RE.match(str(ref).strip())
    if not m:
        return str(ref), None
    if m.group(2) is None:
        return m.group(1), None
    first = int(m.group(2))
    return m.group(1), (first, int(m.group(3) or first))


def _normalize(text: str) -> str:
    return " ".join(text.split())


# --- git helpers ----------------------------------------------------------------


def _git(*args: str) -> subprocess.CompletedProcess:
    return fetch._git(*args, check=False)


def _bare(root: Path, source: guidecfg.Source, git_fetch: bool) -> Path:
    bare = Path(root) / ".guide" / "cache" / f"{source.id}.git"
    if git_fetch:
        try:
            return fetch._update_bare(Path(root), source, [])
        except fetch.FetchError as err:
            raise UpstreamError(f"cannot fetch {source.id}: {err}") from err
    if not bare.is_dir():
        raise UpstreamError(f"source {source.id!r} is not fetched (run make fetch-baseline)")
    return bare


def _default_head(bare: Path, url: str, git_fetch: bool) -> str:
    """Commit at the tip of the repository's default branch."""
    branch = None
    if git_fetch:
        proc = _git("ls-remote", "--symref", "--", url, "HEAD")
        m = re.search(r"^ref:\s*refs/heads/(\S+)\s+HEAD", proc.stdout, re.M) if proc.returncode == 0 else None
        branch = m.group(1) if m else None
    if branch is None:
        proc = _git("-C", str(bare), "symbolic-ref", "--short", "HEAD")
        branch = proc.stdout.strip() if proc.returncode == 0 else None
    for ref in ([f"refs/heads/{branch}"] if branch else []) + ["HEAD"]:
        proc = _git("-C", str(bare), "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}")
        if proc.returncode == 0 and proc.stdout.strip():
            return proc.stdout.strip()
    raise UpstreamError(f"cannot determine the default branch head in {bare}")


def _show(bare: Path, commit: str, path: str) -> str | None:
    proc = _git("-C", str(bare), "show", f"{commit}:{path}")
    return proc.stdout if proc.returncode == 0 else None


def _quote_lines(content: str | None, quote: str | None) -> tuple[int, int] | None:
    """Line range of an exact quote in content (1-based), else None."""
    if not content or not quote:
        return None
    idx = content.find(quote.strip())
    if idx < 0:
        return None
    first = content[:idx].count("\n") + 1
    return first, first + quote.strip().count("\n")


# --- operations -----------------------------------------------------------------


def reverify(root: Path, finding_id: str, git_fetch: bool = True) -> str:
    """Search every location's quote on the default branch head; record the result (FR-032)."""
    root = Path(root)
    header, findings = _load(root)
    finding = _find(findings, finding_id)
    if finding.get("status") in TERMINAL:
        return finding["status"]
    source = _source(root, finding)
    bare = _bare(root, source, git_fetch)
    head = _default_head(bare, fetch.clone_url(source.repo, root), git_fetch)

    problems = []
    locations = finding.get("locations") or []
    if not locations:
        problems.append("no locations recorded")
    for loc in locations:
        path, _ = _split_ref(loc.get("ref", ""))
        quote = loc.get("quote")
        content = _show(bare, head, path)
        if content is None:
            problems.append(f"{path} no longer exists")
        elif not quote:
            problems.append(f"{loc.get('ref')} has no recorded quote, re-verify it by hand")
        elif quote.strip() not in content and _normalize(quote) not in _normalize(content):
            problems.append(f"the quoted text is gone from {path}")

    status = "needs-review" if problems else "reverified"
    finding["status"] = status
    finding["reverified_commit"] = head
    note = finding.get("note") or ""
    if problems:
        finding["note"] = f"{NOTE_PREFIX} at {head[:7]}: " + "; ".join(problems)
    elif note.startswith(NOTE_PREFIX):
        finding.pop("note")
    _save(root, header, findings)
    return status


def _locations_text(finding: dict, source: guidecfg.Source, commit: str, bare: Path | None) -> str:
    slug = source.github_slug
    items = []
    for loc in finding.get("locations") or []:
        path, lines = _split_ref(loc.get("ref", ""))
        quote = loc.get("quote")
        if bare is not None and quote:
            lines = _quote_lines(_show(bare, commit, path), quote) or lines
        span = "" if lines is None else (f"{lines[0]}" if lines[0] == lines[1] else f"{lines[0]}-{lines[1]}")
        label = f"{path}:{span}" if span else path
        if slug:
            anchor = "" if lines is None else (f"#L{lines[0]}" if lines[0] == lines[1]
                                                else f"#L{lines[0]}-L{lines[1]}")
            item = f"- [`{label}`](https://github.com/{slug}/blob/{commit}/{path}{anchor})"
        else:
            item = f"- `{label}` at commit `{commit[:7]}`"
        if quote:
            item += "\n\n" + "\n".join(f"  > {line}" for line in quote.strip().splitlines())
        items.append(item)
    return "\n".join(items) or "- (no locations recorded)"


def _template() -> Path:
    here = Path(__file__).resolve().parent
    for candidate in (here / "templates" / "upstream-issue.md", here.parent / "templates" / "upstream-issue.md"):
        if candidate.is_file():
            return candidate
    raise UpstreamError("upstream-issue.md template not found next to the toolkit")


def draft(root: Path, finding_id: str, run_id: str | None = None) -> Path:
    """Render the issue draft for one re-verified finding into .guide/runs/<run-id>/drafts/."""
    root = Path(root)
    header, findings = _load(root)
    finding = _find(findings, finding_id)
    if finding.get("status") not in DRAFTABLE:
        raise UpstreamError(f"{finding_id} has status {finding.get('status')!r}; "
                            "re-verify it on the default branch before drafting (FR-032)")
    source = _source(root, finding)
    commit = finding.get("reverified_commit") or source.commit
    bare = Path(root) / ".guide" / "cache" / f"{source.id}.git"

    proposal = (finding.get("proposal") or "").strip() or \
        "_Suggested improvement: to be written by the author before this report is approved._"
    if finding.get("kind") == "naming-ux":
        proposal = ("This is a naming or user-experience proposal, filed as an issue for discussion "
                    "rather than as a change, because renaming affects existing users.\n\n" + proposal)
    values = {
        "SUMMARY": str(finding.get("summary", "")).strip(),
        "LOCATIONS": _locations_text(finding, source, commit, bare if bare.is_dir() else None),
        "EVIDENCE": str(finding.get("evidence", "")).strip(),
        "PROPOSAL": proposal,
        "REVERIFIED_COMMIT": commit,
    }
    body = _template().read_text()
    for key, value in values.items():
        body = body.replace("{{" + key + "}}", value)

    run_id = run_id or runs.next_run_id(root, "upstream")
    out = root / ".guide" / "runs" / run_id / "drafts" / f"{finding_id}.md"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(f"# {values['SUMMARY']}\n\n{body}")
    finding["status"] = "drafted"
    _save(root, header, findings)
    return out


@functools.lru_cache(maxsize=256)
def _gh_private(slug: str) -> bool | None:
    """Visibility of a GitHub repository through `gh api` (None when unknown)."""
    proc = fetch.run_gh("api", f"repos/{slug}", "--jq", ".private")
    if proc is None or proc.returncode != 0:
        return None
    value = proc.stdout.strip()
    return True if value == "true" else False if value == "false" else None


def _own_remotes(guide: guidecfg.Guide) -> list[str]:
    root = Path(guide.root).resolve()
    top = _git("-C", str(root), "rev-parse", "--show-toplevel")
    if top.returncode != 0 or Path(top.stdout.strip()).resolve() != root:
        return []
    proc = _git("-C", str(root), "remote", "-v")
    return sorted({line.split()[1] for line in proc.stdout.splitlines() if len(line.split()) >= 2})


def _internal_markers(guide: guidecfg.Guide) -> set[str]:
    markers = {"internal"}
    try:
        adapter = guidecfg.load_adapter(guide.root, required=False)
    except guidecfg.ConfigError:
        adapter = None
    if adapter is not None and adapter.internal_root:
        markers.add(Path(adapter.internal_root).name)
    return markers


def sanitize(text: str, guide: guidecfg.Guide,
             private_check: Callable[[str], bool | None] | None = None,
             own_remotes: list[str] | None = None) -> list[Problem]:
    """Problems that keep a draft from going to a public repository (FR-035)."""
    check = private_check or _gh_private
    problems: list[Problem] = []
    for term in guide.confidential_terms:
        for m in re.finditer(re.escape(term), text, re.IGNORECASE):
            problems.append(Problem("term", m.group(0), f"confidential term {m.group(0)!r}"))
    for pattern in guide.confidential_patterns:
        for m in re.finditer(pattern, text):
            problems.append(Problem("pattern", m.group(0), f"text {m.group(0)!r} matches confidential pattern {pattern!r}"))
    for marker in sorted(_internal_markers(guide)):
        m_esc = re.escape(marker)
        for regex in (rf"xref:{m_esc}:\S*", rf"https?://\S*/{m_esc}/\S*", rf"\]\((?:\.{{1,2}}/)*{m_esc}/[^)]*\)"):
            for m in re.finditer(regex, text):
                problems.append(Problem("internal-link", m.group(0), f"link into the internal part: {m.group(0)}"))

    if classify.redact(text) != text:
        # Report per line; a multi-line secret (a PEM block) that no single line reveals
        # is still reported once, so the draft never passes.
        flagged = False
        for line in text.splitlines():
            redacted_line = classify.redact(line)
            if redacted_line != line:
                flagged = True
                problems.append(Problem("secret", line.strip()[:80],
                                        f"looks like a credential or token: {redacted_line.strip()[:80]}"))
        if not flagged:
            problems.append(Problem("secret", "", "contains a multi-line credential (for example a private key)"))

    own = own_remotes if own_remotes is not None else _own_remotes(guide)
    own_urls = {guidecfg.web_url(r).lower() for r in own}
    own_slugs = {u.split("github.com/", 1)[1] for u in own_urls if "github.com/" in u}
    for url in sorted(own_urls):
        if url and url.lower() in text.lower():
            problems.append(Problem("own-repo", url, f"link to the guide project's own repository {url}"))

    seen: set[str] = set()
    for m in GITHUB_URL_RE.finditer(text):
        owner, name = m.group(1), re.sub(r"\.git$", "", m.group(2))
        slug = f"{owner}/{name}"
        if owner.lower() in GITHUB_NON_REPO or slug.lower() in seen:
            continue
        seen.add(slug.lower())
        if slug.lower() in own_slugs:
            if not any(p.kind == "own-repo" for p in problems):
                problems.append(Problem("own-repo", slug, f"link to the guide project's own repository {slug}"))
            continue
        private = check(slug)
        if private is True:
            problems.append(Problem("private-repo", slug, f"link to the private repository {slug}"))
        elif private is None:
            problems.append(Problem("private-repo", slug,
                                    f"could not determine whether {slug} is public; check the link", "warning"))
    return problems


def _overlap(a: dict, b: dict) -> bool:
    if a.get("source") != b.get("source"):
        return False
    for la in a.get("locations") or []:
        for lb in b.get("locations") or []:
            qa, qb = _normalize(la.get("quote") or ""), _normalize(lb.get("quote") or "")
            if qa and qa == qb:
                return True
            pa, ra = _split_ref(la.get("ref", ""))
            pb, rb = _split_ref(lb.get("ref", ""))
            if pa != pb:
                continue
            if ra is None or rb is None or (ra[0] <= rb[1] and rb[0] <= ra[1]):
                return True
    return False


def _number(f: dict) -> int:
    m = re.match(r"^UF-(\d+)$", str(f.get("id", "")))
    return int(m.group(1)) if m else 10**9


def dedupe(root: Path) -> list[Merge]:
    """Merge findings about the same upstream problem into the earliest one (spec edge case)."""
    root = Path(root)
    header, findings = _load(root)
    kept: list[dict] = []
    merges: list[Merge] = []
    for f in sorted(findings, key=_number):
        if f.get("status") not in ACTIVE:
            continue
        target = next((k for k in kept if _overlap(k, f)), None)
        if target is None or f.get("status") not in MERGEABLE:
            kept.append(f)
            continue
        f["status"] = "merged"
        f["merged_into"] = target["id"]
        merges.append(Merge(kept=target["id"], merged=f["id"]))
    if merges:
        _save(root, header, findings)
    return merges


def _keywords(summary: str, limit: int = 6) -> str:
    words: list[str] = []
    for w in re.findall(r"[A-Za-z0-9_.-]+", summary.lower()):
        w = w.strip(".-")
        if len(w) < 3 or w in STOPWORDS or w in words:
            continue
        words.append(w)
    return " ".join(words[:limit])


def _gh_search(slug: str, query: str) -> list[dict]:
    if shutil.which("gh") is None:
        print("note: gh is not installed; cannot search for existing issues", file=sys.stderr)
        return []
    proc = fetch.run_gh("search", "issues", "--repo", slug, query, "--json", "url,title", "--limit", "5")
    if proc is None or proc.returncode != 0:
        reason = "timed out" if proc is None else proc.stderr.strip()
        print(f"note: issue search in {slug} failed: {reason}", file=sys.stderr)
        return []
    try:
        return json.loads(proc.stdout or "[]")
    except json.JSONDecodeError:
        return []


def existing(finding: dict, search: Callable[[str, str], list[dict]] | None = None,
             root: Path = Path(".")) -> list[str]:
    """URLs of upstream issues that may already report this finding (FR-036)."""
    source = guidecfg.load_baseline(root).source(finding.get("source"))
    slug = source.github_slug if source else None
    if not slug:
        print(f"note: {finding.get('id')}: source {finding.get('source')!r} is not on GitHub; "
              "no existing-issue search", file=sys.stderr)
        return []
    results = (search or _gh_search)(slug, _keywords(str(finding.get("summary", "")))) or []
    return [r["url"] for r in results if isinstance(r, dict) and r.get("url")]


TRANSITIONS = {
    "collected": {"reverified", "needs-review", "dropped", "merged"},
    "needs-review": {"reverified", "dropped", "merged"},
    "reverified": {"drafted", "dropped", "merged", "needs-review"},
    "drafted": {"approved", "rejected", "drafted", "needs-review"},
    "approved": {"filed", "rejected"},
}


def record(root: Path, finding_id: str, status: str, filed: str | None = None,
           note: str | None = None) -> dict:
    """Move a finding to a new status, enforcing the transitions of data-model.md.

    Nothing reaches `filed` without `approved` (FR-033); `filed` needs the URL.
    """
    header, findings = _load(root)
    f = _find(findings, finding_id)
    current = f.get("status", "collected")
    if status not in TRANSITIONS.get(current, set()):
        raise UpstreamError(f"{finding_id}: cannot move from {current!r} to {status!r}")
    if status == "filed" and not filed:
        raise UpstreamError(f"{finding_id}: status filed needs the issue or pull request URL (--filed)")
    if status in ("dropped", "rejected") and not note:
        raise UpstreamError(f"{finding_id}: status {status} needs a --note with the reason")
    f["status"] = status
    if filed:
        f["filed"] = filed
    if note:
        f["note"] = note
    problems = guidecfg.check_finding(f)
    if problems:
        raise UpstreamError(f"{finding_id}: " + "; ".join(problems))
    _save(root, header, findings)
    return f


# --- CLI ------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Upstream findings: reverify, draft, sanitize, dedupe, existing")
    sub = parser.add_subparsers(dest="cmd", required=True)
    rv = sub.add_parser("reverify")
    rv.add_argument("--no-fetch", action="store_true", help="use the cached clone as is")
    rv.add_argument("--all", action="store_true", help="every collected, reverified, or drafted finding")
    rv.add_argument("ids", nargs="*")
    dr = sub.add_parser("draft")
    dr.add_argument("--run-id")
    dr.add_argument("--offline", action="store_true", help="do not ask GitHub about repository visibility")
    dr.add_argument("ids", nargs="+")
    sa = sub.add_parser("sanitize")
    sa.add_argument("--offline", action="store_true")
    sa.add_argument("files", nargs="+", type=Path)
    sub.add_parser("dedupe")
    ex = sub.add_parser("existing")
    ex.add_argument("ids", nargs="+")
    rc = sub.add_parser("record", help="set a finding's status (approved, rejected, filed, dropped, ...)")
    rc.add_argument("id")
    rc.add_argument("--status", required=True, choices=sorted(guidecfg.FINDING_STATUSES))
    rc.add_argument("--filed", help="issue or pull request URL (required for filed)")
    rc.add_argument("--note")
    for p in sub.choices.values():
        p.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args(argv)
    root = args.root
    offline_check = (lambda slug: None)

    try:
        if args.cmd == "reverify":
            ids = args.ids or []
            if args.all:
                ids = [f["id"] for f in guidecfg.load_findings(root)
                       if f.get("status") in ("collected", "reverified", "drafted", "needs-review")]
            if not ids:
                parser.error("give finding ids or --all")
            for fid in ids:
                status = reverify(root, fid, git_fetch=not args.no_fetch)
                f = _find(guidecfg.load_findings(root), fid)
                note = f": {f['note']}" if status == "needs-review" and f.get("note") else ""
                print(f"{fid} {status} {str(f.get('reverified_commit', ''))[:7]}{note}".rstrip())
            return 0
        if args.cmd == "draft":
            guide = guidecfg.load_guide(root)
            failed = False
            for fid in args.ids:
                path = draft(root, fid, args.run_id)
                problems = sanitize(path.read_text(), guide, offline_check if args.offline else None)
                for p in problems:
                    print(f"{path}: {p}", file=sys.stderr)
                failed |= any(p.severity == "error" for p in problems)
                print(path)
            return 1 if failed else 0
        if args.cmd == "sanitize":
            guide = guidecfg.load_guide(root)
            failed = False
            for file in args.files:
                for p in sanitize(file.read_text(), guide, offline_check if args.offline else None):
                    print(f"{file}: {p}")
                    failed |= p.severity == "error"
            return 1 if failed else 0
        if args.cmd == "dedupe":
            merges = dedupe(root)
            for m in merges:
                print(f"{m.merged} merged into {m.kept}")
            if not merges:
                print("no duplicates")
            return 0
        if args.cmd == "record":
            f = record(root, args.id, args.status, args.filed, args.note)
            print(f"{args.id} {f['status']}" + (f" {f['filed']}" if f.get("filed") else ""))
            return 0
        if args.cmd == "existing":
            findings = guidecfg.load_findings(root)
            for fid in args.ids:
                urls = existing(_find(findings, fid), root=root)
                print(f"{fid} " + (" ".join(urls) if urls else "none"))
            return 0
    except (UpstreamError, guidecfg.ConfigError) as err:
        print(f"error: {err}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
