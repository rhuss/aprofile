#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Pin, fetch, and re-pin the sources recorded in baseline.yaml.

  pin --source ID=REPO[@REF]...   resolve REF (default HEAD) to a full SHA and write baseline.yaml
  fetch [--allow-unverified]      blobless bare clones plus worktrees at the pinned commits
  rebaseline --to ID=REF...       move pins to new revisions, refresh change-request states

Layout under the guide project:
  .guide/cache/<id>.git   blobless bare clone
  .guide/cache/<id>/      detached worktree at the pinned commit

Every git call runs as `git -C <path>` from the current directory; nothing here
changes the working directory or runs commands inside the cache.
"""
from __future__ import annotations

import argparse
import datetime as _dt
import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from typing import Callable

import yaml

import guidecfg

SHA_RE = re.compile(r"^[0-9a-f]{40}$")
GITHUB_SHORT_RE = re.compile(r"^[\w.-]+/[\w.-]+$")
DEFAULT_HEADER = "# Pinned sources and change requests (the baseline). Written by tools/guide/fetch.py.\n"
GIT_ENV = {**os.environ, "GIT_TERMINAL_PROMPT": "0"}


class FetchError(Exception):
    """A source could not be pinned or fetched."""


# --- helpers ----------------------------------------------------------------


GIT_TIMEOUT = int(os.environ.get("GUIDE_GIT_TIMEOUT", "600"))


def _git(*args: str, check: bool = True, timeout: int | None = None) -> subprocess.CompletedProcess:
    """Run git with prompts disabled and a timeout (a hung remote must not hang a run or CI)."""
    try:
        proc = subprocess.run(["git", *args], capture_output=True, text=True, env=GIT_ENV,
                              timeout=timeout or GIT_TIMEOUT)
    except subprocess.TimeoutExpired:
        proc = subprocess.CompletedProcess(["git", *args], 124, "",
                                           f"timed out after {timeout or GIT_TIMEOUT}s (set GUIDE_GIT_TIMEOUT to change)")
    if check and proc.returncode != 0:
        raise FetchError(f"git {' '.join(args)}: {proc.stderr.strip() or proc.stdout.strip()}")
    return proc


def parse_spec(spec: str) -> tuple[str, str, str | None]:
    """Split `ID=REPO[@REF]` into (id, repo, ref or None)."""
    if "=" not in spec:
        raise FetchError(f"bad source spec {spec!r} (expected ID=REPO[@REF])")
    source_id, rest = spec.split("=", 1)
    repo, ref = rest, None
    if "@" in rest:
        head, tail = rest.rsplit("@", 1)
        # git@host:owner/name has its only "@" before the host; refs never contain ":"
        if ":" not in tail and not (head == "git" or head.endswith("://git")):
            repo, ref = head, tail
    if not source_id or not repo:
        raise FetchError(f"bad source spec {spec!r} (expected ID=REPO[@REF])")
    return source_id, repo, ref or None


def clone_url(repo: str, root: Path) -> str:
    """URL or path git can clone for a baseline `repo` value."""
    if guidecfg.URL_RE.match(repo) or repo.startswith("git@"):
        return repo
    path = Path(repo).expanduser()
    if not path.is_absolute():
        path = Path(root) / path
    if path.exists():
        return str(path.resolve())
    if GITHUB_SHORT_RE.match(repo):
        return f"https://github.com/{repo}.git"
    return repo


def _today() -> str:
    return _dt.date.today().isoformat()


def _read_raw(root: Path) -> tuple[str, dict]:
    path = Path(root) / "baseline.yaml"
    if not path.is_file():
        return DEFAULT_HEADER, {"captured": _today(), "sources": [], "change_requests": []}
    text = path.read_text()
    header_lines = []
    for line in text.splitlines(keepends=True):
        if line.startswith("#"):
            header_lines.append(line)
        else:
            break
    data = yaml.safe_load(text) or {}
    return "".join(header_lines) or DEFAULT_HEADER, data


def _write_raw(root: Path, header: str, data: dict) -> None:
    for key in ("captured",):
        value = data.get(key)
        if isinstance(value, (_dt.date, _dt.datetime)):
            data[key] = value.strftime("%Y-%m-%d")
    ordered = {k: data[k] for k in ("captured", "sources", "change_requests") if k in data}
    ordered.update({k: v for k, v in data.items() if k not in ordered})
    if not ordered.get("change_requests"):
        ordered.pop("change_requests", None)
    body = yaml.safe_dump(ordered, sort_keys=False, allow_unicode=True)
    # Write atomically: an interrupted run must never leave a truncated baseline.
    target = Path(root) / "baseline.yaml"
    tmp = target.with_name(".baseline.yaml.tmp")
    tmp.write_text(header + body)
    os.replace(tmp, target)


def _ls_remote(url: str, ref: str | None) -> str:
    """Resolve a ref on a remote to a full commit SHA."""
    proc = _git("ls-remote", "--", url, check=False)
    if proc.returncode != 0:
        raise FetchError(f"cannot read {url}: {proc.stderr.strip()}")
    refs: dict[str, str] = {}
    for line in proc.stdout.splitlines():
        sha, _, name = line.partition("\t")
        refs[name] = sha
    want = ref or "HEAD"
    for name in (want, f"refs/heads/{want}", f"refs/tags/{want}^{{}}", f"refs/tags/{want}"):
        if name in refs:
            return refs[name]
    raise FetchError(f"ref {want!r} not found in {url}")


def _cache(root: Path) -> Path:
    return Path(root) / ".guide" / "cache"


def _update_bare(root: Path, source: guidecfg.Source, change_requests: list[guidecfg.ChangeRequest]) -> Path:
    """Clone (blobless, bare) or update the source's cache; return the bare repository path."""
    url = clone_url(source.repo, root)
    bare = _cache(root) / f"{source.id}.git"
    if not bare.is_dir():
        bare.parent.mkdir(parents=True, exist_ok=True)
        proc = _git("clone", "--quiet", "--bare", "--filter=blob:none", "--", url, str(bare), check=False)
        if proc.returncode != 0:
            shutil.rmtree(bare, ignore_errors=True)
            raise FetchError(f"cannot clone {source.repo}: {proc.stderr.strip()}")
    else:
        _git("-C", str(bare), "config", "remote.origin.url", url)
    proc = _git("-C", str(bare), "fetch", "--quiet", "origin",
                "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*", check=False)
    if proc.returncode != 0:
        raise FetchError(f"cannot fetch {source.repo}: {proc.stderr.strip()}")
    if source.github_slug:
        for cr in change_requests:
            if cr.source == source.id:
                _git("-C", str(bare), "fetch", "--quiet", "origin",
                     f"+refs/pull/{cr.number}/head:refs/pull/{cr.number}/head", check=False)
    return bare


def _ensure_commit(bare: Path, commit: str) -> None:
    if _git("-C", str(bare), "cat-file", "-e", f"{commit}^{{commit}}", check=False).returncode == 0:
        return
    _git("-C", str(bare), "fetch", "--quiet", "origin", commit, check=False)
    if _git("-C", str(bare), "cat-file", "-e", f"{commit}^{{commit}}", check=False).returncode != 0:
        raise FetchError(f"pinned commit {commit} no longer exists upstream; pin a new revision")


def _place_worktree(root: Path, source_id: str, bare: Path, commit: str) -> Path:
    worktree = (_cache(root) / source_id).resolve()
    if (worktree / ".git").exists():
        _git("-C", str(worktree), "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", commit)
    else:
        if worktree.exists():
            shutil.rmtree(worktree)
        _git("-C", str(bare), "worktree", "prune")
        _git("-C", str(bare), "worktree", "add", "--quiet", "--detach", str(worktree), commit)
    return worktree


def _resolve_in_bare(bare: Path, ref: str) -> str | None:
    for name in (ref, f"refs/heads/{ref}", f"refs/tags/{ref}"):
        proc = _git("-C", str(bare), "rev-parse", "--verify", "--quiet", f"{name}^{{commit}}", check=False)
        if proc.returncode == 0 and SHA_RE.match(proc.stdout.strip()):
            return proc.stdout.strip()
    return None


# --- operations ---------------------------------------------------------------


def pin(root: Path, specs: list[str]) -> guidecfg.Baseline:
    """Resolve each `ID=REPO[@REF]` to a full SHA and record it in baseline.yaml."""
    root = Path(root)
    header, data = _read_raw(root)
    entries = data.setdefault("sources", []) or []
    data["sources"] = entries
    for spec in specs:
        source_id, repo, ref = parse_spec(spec)
        commit = ref if ref and SHA_RE.match(ref) else _ls_remote(clone_url(repo, root), ref)
        entry = next((e for e in entries if e.get("id") == source_id), None)
        if entry is None:
            entry = {"id": source_id}
            entries.append(entry)
        entry["repo"] = repo
        entry["ref"] = ref or "HEAD"
        entry["commit"] = commit
        # keep id, repo, ref, commit first; preserve status and attributes
        rest = {k: v for k, v in entry.items() if k not in ("id", "repo", "ref", "commit")}
        entry.clear()
        entry.update({"id": source_id, "repo": repo, "ref": ref or "HEAD", "commit": commit, **rest})
    data["captured"] = _today()
    _write_raw(root, header, data)
    return guidecfg.load_baseline(root)


def fetch(root: Path, allow_unverified: bool = False) -> dict[str, str]:
    """Fetch every source into .guide/cache and place a worktree at its pinned commit."""
    root = Path(root)
    baseline = guidecfg.load_baseline(root)
    status: dict[str, str] = {}
    failures: dict[str, str] = {}
    for source in baseline.sources:
        try:
            bare = _update_bare(root, source, baseline.change_requests)
            _ensure_commit(bare, source.commit)
            _place_worktree(root, source.id, bare, source.commit)
            status[source.id] = "verified"
        except FetchError as err:
            failures[source.id] = str(err)
            if allow_unverified:
                status[source.id] = "unverified"

    header, data = _read_raw(root)
    changed = False
    for entry in data.get("sources") or []:
        new = status.get(entry.get("id"))
        if new and entry.get("status", "verified") != new:
            entry["status"] = new
            changed = True
    if changed:
        _write_raw(root, header, data)

    if failures and not allow_unverified:
        lines = "; ".join(f"{sid}: {msg}" for sid, msg in failures.items())
        raise FetchError(f"cannot fetch source(s) {', '.join(failures)}: {lines}")
    for sid, msg in failures.items():
        print(f"warning: source {sid} marked unverified: {msg}", file=sys.stderr)
    return status


GH_TIMEOUT = int(os.environ.get("GUIDE_GH_TIMEOUT", "60"))


def run_gh(*args: str) -> subprocess.CompletedProcess | None:
    """Run the gh CLI with a timeout; None when gh is missing or does not answer in time."""
    if shutil.which("gh") is None:
        return None
    try:
        return subprocess.run(["gh", *args], capture_output=True, text=True, timeout=GH_TIMEOUT)
    except subprocess.TimeoutExpired:
        return None


def _gh_pull(slug: str, number: int) -> dict | None:
    """State of a GitHub pull request through `gh api` (None when unavailable)."""
    proc = run_gh("api", f"repos/{slug}/pulls/{number}",
                  "--jq", "{state: .state, merged: .merged, head: .head.sha}")
    if proc is None or proc.returncode != 0:
        return None
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return None


def refresh_change_requests(data: dict, slugs: dict[str, str | None],
                            gh: Callable[[str, int], dict | None] = _gh_pull) -> list[str]:
    """Update change-request entries in raw baseline data from GitHub; return warnings."""
    warnings = []
    for cr in data.get("change_requests") or []:
        slug = slugs.get(cr.get("source"))
        if not slug:
            continue
        info = gh(slug, cr.get("number"))
        if not info:
            warnings.append(f"change request {cr.get('source')}#{cr.get('number')}: state not refreshed (gh unavailable or failed)")
            continue
        if info.get("merged"):
            cr["state"] = "merged"
            cr["tier"] = "on-main"
        elif info.get("state") == "closed":
            cr["state"] = "closed"
        else:
            cr["state"] = "open"
        head = info.get("head")
        if isinstance(head, str) and SHA_RE.match(head) and cr["state"] == "open":
            cr["head"] = head
    return warnings


def rebaseline(root: Path, targets: list[str],
               gh: Callable[[str, int], dict | None] = _gh_pull) -> guidecfg.Baseline:
    """Move the pins of the named sources to new revisions and refresh change requests."""
    root = Path(root)
    baseline = guidecfg.load_baseline(root)
    wanted: dict[str, str] = {}
    for spec in targets:
        if "=" not in spec:
            raise FetchError(f"bad target {spec!r} (expected ID=REF)")
        source_id, ref = spec.split("=", 1)
        if baseline.source(source_id) is None:
            raise FetchError(f"unknown source {source_id!r} (not in baseline.yaml)")
        wanted[source_id] = ref

    commits: dict[str, str] = {}
    for source_id, ref in wanted.items():
        source = baseline.source(source_id)
        bare = _update_bare(root, source, baseline.change_requests)
        commit = _resolve_in_bare(bare, ref) if not SHA_RE.match(ref) else ref
        if commit is None:
            commit = _ls_remote(clone_url(source.repo, root), ref)
        _ensure_commit(bare, commit)
        _place_worktree(root, source_id, bare, commit)
        commits[source_id] = commit

    header, data = _read_raw(root)
    for entry in data.get("sources") or []:
        sid = entry.get("id")
        if sid in commits:
            entry["commit"] = commits[sid]
            if not SHA_RE.match(wanted[sid]):
                entry["ref"] = wanted[sid]
            entry["status"] = "verified"
    slugs = {s.id: s.github_slug for s in baseline.sources}
    for warning in refresh_change_requests(data, slugs, gh):
        print(f"warning: {warning}", file=sys.stderr)
    data["captured"] = _today()
    _write_raw(root, header, data)
    return guidecfg.load_baseline(root)


# --- CLI --------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Pin and fetch the sources in baseline.yaml")
    sub = parser.add_subparsers(dest="cmd", required=True)
    p = sub.add_parser("pin")
    p.add_argument("--source", action="append", required=True, metavar="ID=REPO[@REF]")
    p.add_argument("--root", type=Path, default=Path("."))
    f = sub.add_parser("fetch")
    f.add_argument("--allow-unverified", action="store_true")
    f.add_argument("--root", type=Path, default=Path("."))
    r = sub.add_parser("rebaseline")
    r.add_argument("--to", action="append", required=True, metavar="ID=REF")
    r.add_argument("--root", type=Path, default=Path("."))
    args = parser.parse_args(argv)

    try:
        if args.cmd == "pin":
            baseline = pin(args.root, args.source)
            for s in baseline.sources:
                print(f"{s.id}: {s.commit} ({s.ref})")
        elif args.cmd == "fetch":
            status = fetch(args.root, args.allow_unverified)
            for sid, st in status.items():
                print(f"{sid}: {st}")
        else:
            baseline = rebaseline(args.root, args.to)
            for s in baseline.sources:
                print(f"{s.id}: {s.commit}")
            print(f"captured: {baseline.captured}")
    except (FetchError, guidecfg.ConfigError) as err:
        print(f"error: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
