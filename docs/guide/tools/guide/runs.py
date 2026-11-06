#!/usr/bin/env python3
# /// script
# requires-python = ">=3.11"
# dependencies = ["pyyaml"]
# ///
"""Run records: the progress ledger, run summaries, and collected upstream findings.

The superpowers ledger (`.superpowers/sdd/<plan>/progress.md`) is read, never written.
Its first line names the plan (`# SDD ledger <separator> plan: <plan file>`), which is
the run identity (FR-013a).

Usage:
  uv run tools/guide/runs.py summary --workspace DIR --run-id ID [--command create]
        [--rigor standard] [--model NAME] [--effort high] [--root DIR]
  uv run tools/guide/runs.py findings-add [--root DIR] --json FILE|-
  uv run tools/guide/runs.py findings-add [--root DIR] --source ID --kind KIND
        --location PATH:LINES [--quote TEXT]... --summary TEXT --evidence TEXT
  uv run tools/guide/runs.py resume --workspace DIR [--plan FILE]   (prints N or done)
"""
from __future__ import annotations

import argparse
import datetime as _dt
import json
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path

import yaml

import guidecfg

IDENTITY_RE = re.compile(r"^#\s*SDD ledger\b.*?\bplan:\s*(\S.*?)\s*$")
TASK_RE = re.compile(r"^Task\s+(\d+):\s*(.*)$")
RULING_RE = re.compile(r"^Ruling(?:\s+R(\d+))?([^:]*):\s*(.*)$")
TASK_RULING_RE = re.compile(r"^Ruling(?:\s+R(\d+))?\s*:\s*(.*)$")
FIX_RE = re.compile(r"^fix round\s+(\d+)\s*/\s*\d+\b(.*)$")
COMMITS_RE = re.compile(r"\bcommits?\s+([0-9a-f]{4,40}\.\.[0-9a-f]{4,40})")
PARKED_COUNT_RE = re.compile(r"\b(\d+)\s+parked\b")
OUTCOMES = ("match", "mismatch", "not-evaluable", "skipped", "confirmed-and-run", "declined")


class FindingError(ValueError):
    """An upstream finding does not follow the schema."""


@dataclass
class TaskState:
    status: str = "dispatched"   # dispatched | in review | fix round | complete
    verdict: str = ""
    fix_rounds: int = 0
    commits: str = ""
    parked: int = 0
    review_lines: int = 0
    reviewed: bool = False

    @property
    def review_dispatches(self) -> int:
        initial = max(self.review_lines, 1 if self.reviewed else 0)
        return initial + self.fix_rounds


@dataclass
class Ruling:
    id: str
    text: str
    task: int | None = None


@dataclass
class Deferred:
    task: int
    text: str


@dataclass
class Ledger:
    plan: str | None
    tasks: dict[int, TaskState] = field(default_factory=dict)
    rulings: list[Ruling] = field(default_factory=list)
    deferred: list[Deferred] = field(default_factory=list)
    rejected: list[str] = field(default_factory=list)

    @property
    def review_dispatches(self) -> int:
        return sum(t.review_dispatches for t in self.tasks.values())


def parse_ledger(path: Path) -> Ledger:
    """Read a superpowers progress ledger (tolerant: unknown lines are ignored)."""
    path = Path(path)
    lines = path.read_text(errors="replace").splitlines() if path.is_file() else []
    plan = None
    for line in lines[:3]:
        m = IDENTITY_RE.match(line.strip())
        if m:
            plan = m.group(1)
            break
    led = Ledger(plan=plan)
    highest = 0

    def add_ruling(number: str | None, text: str, task: int | None) -> None:
        nonlocal highest
        if number:
            n = int(number)
        else:
            n = highest + 1
        highest = max(highest, n)
        led.rulings.append(Ruling(f"R{n}", text.strip(), task))

    for raw in lines:
        line = raw.strip()
        if not line:
            continue
        if re.search(r"rejected[- ]by[- ]source", line, re.I):
            led.rejected.append(line)
        m = RULING_RE.match(line)
        if m:
            qualifier = m.group(2).strip()
            text = f"{qualifier}: {m.group(3)}" if qualifier else m.group(3)
            add_ruling(m.group(1), text, None)
            continue
        m = TASK_RE.match(line)
        if not m:
            continue
        n, rest = int(m.group(1)), m.group(2).strip()
        task = led.tasks.setdefault(n, TaskState())
        low = rest.lower()
        rm = TASK_RULING_RE.match(rest)
        if rm:
            add_ruling(rm.group(1), rm.group(2), n)
            continue
        if low.startswith("dispatched"):
            if task.status != "complete":
                task.status = "dispatched"
        elif low.startswith("review"):
            verdict = rest[len("review"):].strip()
            if verdict.lower().startswith("dispatched"):
                if task.status != "complete":
                    task.status = "in review"
                continue
            verdict = re.split(r"\s*[(.]", verdict, maxsplit=1)[0].strip()
            if verdict.lower().startswith("skipped"):
                # `review skipped (rigor fast)`: no reviewer was dispatched (SC-010)
                task.verdict = "not reviewed (fast)"
                continue
            task.review_lines += 1
            task.verdict = verdict
            if task.status != "complete":
                task.status = "in review"
        elif low.startswith("fix round"):
            fm = FIX_RE.match(rest)
            if fm and not fm.group(2).strip().lower().startswith("dispatched"):
                task.fix_rounds = max(task.fix_rounds, int(fm.group(1)))
                if task.status != "complete":
                    task.status = "fix round"
            cm = COMMITS_RE.search(rest)
            if cm and task.status != "complete":
                task.commits = cm.group(1)
        elif low.startswith("minor") and "deferred" in low:
            text = rest.split(":", 1)[1].strip() if ":" in rest else rest
            led.deferred.append(Deferred(n, text))
        elif low.startswith("parked"):
            task.parked += 1
            if "ruling:" in low:
                add_ruling(None, rest.split("Ruling:", 1)[-1] if "Ruling:" in rest else rest, n)
        elif low.startswith("complete"):
            task.status = "complete"
            cm = COMMITS_RE.search(rest)
            if cm:
                task.commits = cm.group(1)
            pm = PARKED_COUNT_RE.search(rest)
            if pm:
                task.parked = max(task.parked, int(pm.group(1)))
            fr = re.search(r"after\s+(\d+)\s+fix round", rest)
            if fr:
                task.fix_rounds = max(task.fix_rounds, int(fr.group(1)))
            if "review" in low and not re.search(r"no review|not reviewed", low) \
                    and task.verdict != "not reviewed (fast)":
                task.reviewed = True
            if "review clean" in low:
                task.verdict = "clean"
            elif task.parked:
                task.verdict = "parked"
            elif "no review" in low:
                task.verdict = "not reviewed"
    return led


def resume_point(ledger: Ledger, total: int | None = None) -> int | None:
    """First task to continue with, or None when the run is done (FR-013a)."""
    unfinished = sorted(n for n, t in ledger.tasks.items() if t.status != "complete")
    if unfinished:
        return unfinished[0]
    done = set(ledger.tasks)
    if total:
        for n in range(1, total + 1):
            if n not in done:
                return n
    return None


def plan_task_count(plan: Path) -> int:
    """Number of `Task N` headings in a superpowers plan (outside code fences)."""
    count = 0
    infence = False
    for line in Path(plan).read_text(errors="replace").splitlines():
        if line.startswith("```"):
            infence = not infence
        elif not infence and re.match(r"^#+\s+Task\s+\d+", line):
            count += 1
    return count


# --- run summary ---------------------------------------------------------------


def _template() -> Path:
    here = Path(__file__).resolve().parent
    for candidate in (here / "templates" / "run-summary.md", here.parent / "templates" / "run-summary.md"):
        if candidate.is_file():
            return candidate
    raise FileNotFoundError("run-summary.md template not found next to the toolkit")


def _baseline_text(root: Path) -> str:
    try:
        baseline = guidecfg.load_baseline(root)
    except guidecfg.ConfigError:
        return "No baseline recorded."
    lines = [f"- `{s.id}`: {s.repo} at `{s.commit[:7]}` ("
             + (f"ref {s.ref}, " if s.ref else "") + f"status {s.status})"
             for s in baseline.sources]
    lines.append(f"- captured {baseline.captured}")
    for cr in baseline.change_requests:
        lines.append(f"- change request {cr.source}#{cr.number}: {cr.state}, tier {cr.tier}, head `{cr.head[:7]}`")
    return "\n".join(lines)


def _validation_text(root: Path, run_id: str) -> str:
    path = root / ".guide" / "runs" / run_id / "validation.md"
    if not path.is_file():
        return "Not run."
    counts = {o: 0 for o in OUTCOMES}
    for line in path.read_text(errors="replace").splitlines():
        if not line.startswith("|"):
            continue
        for cell in (c.strip() for c in line.strip("|").split("|")):
            if cell in counts:
                counts[cell] += 1
    parts = [f"{k}: {v}" for k, v in counts.items() if v]
    return (", ".join(parts) if parts else "No results recorded.") + \
        f" (details in `.guide/runs/{run_id}/validation.md`, local only)"


def summary(root: Path, plan_workspace: Path, run_id: str, **meta) -> Path:
    """Write runs/<run-id>.md from the ledger in plan_workspace."""
    root = Path(root)
    led = parse_ledger(Path(plan_workspace) / "progress.md")
    rigor = meta.get("rigor")
    if not rigor:
        try:
            rigor = guidecfg.load_guide(root).rigor
        except guidecfg.ConfigError:
            rigor = "standard"

    rows = []
    for n in sorted(led.tasks):
        t = led.tasks[n]
        verdict = t.verdict or ("not reviewed" if t.status == "complete" else "pending")
        extra = f", {t.parked} parked" if t.parked else ""
        rows.append(f"| {n} | {t.status} | {verdict}{extra} | {t.fix_rounds} | {t.commits or '-'} |")
    tasks = "\n".join(rows) if rows else "| - | no tasks recorded | - | - | - |"

    rulings = "\n".join(f"- **{r.id}**{f' (task {r.task})' if r.task else ''}: {r.text}"
                        for r in led.rulings) or "None."
    rejected = "\n".join(f"- {line}" for line in led.rejected) or "None."
    deferred = "\n".join(f"- Task {d.task}: {d.text}" for d in led.deferred) or "None."
    collected = [f for f in guidecfg.load_findings(root) if f.get("status") == "collected"]
    upstream = "\n".join(
        f"- {f.get('id')} ({f.get('source')}, {f.get('kind')}): {f.get('summary')}" for f in collected
    ) or "None collected."

    current = _baseline_text(root)
    values = {
        "RUN_ID": run_id,
        "COMMAND": meta.get("command") or "create",
        "RIGOR": rigor,
        "DATE": meta.get("date") or _dt.date.today().isoformat(),
        "PLAN": led.plan or "unknown",
        "MODEL": meta.get("model") or "not recorded",
        "EFFORT": meta.get("effort") or ("xhigh" if rigor == "strict" else "high"),
        "REVIEW_DISPATCHES": str(led.review_dispatches),
        "BASELINE_BEFORE": meta.get("baseline_before") or current,
        "BASELINE_AFTER": meta.get("baseline_after") or current,
        "TASKS": tasks,
        "RULINGS": rulings,
        "REJECTED_FINDINGS": rejected,
        "DEFERRED_MINORS": deferred,
        "UPSTREAM_FINDINGS": upstream,
        "VALIDATION": _validation_text(root, run_id),
    }
    text = _template().read_text()
    for key, value in values.items():
        text = text.replace("{{" + key + "}}", value)
    out = root / "runs" / f"{run_id}.md"
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(text)
    return out


def next_run_id(root: Path, command: str, date: str | None = None) -> str:
    """`<YYYY-MM-DD>-<command>[-<n>]`, unique within runs/."""
    date = date or _dt.date.today().isoformat()
    base = f"{date}-{command}"
    runs_dir = Path(root) / "runs"
    if not (runs_dir / f"{base}.md").exists():
        return base
    n = 2
    while (runs_dir / f"{base}-{n}.md").exists():
        n += 1
    return f"{base}-{n}"


# --- upstream findings -----------------------------------------------------------


def findings_add(root: Path, finding: dict) -> str:
    """Validate and append one upstream finding; return its new UF-<n> id."""
    root = Path(root)
    entry = dict(finding)
    entry.setdefault("status", "collected")
    existing = guidecfg.load_findings(root)
    numbers = [int(m.group(1)) for f in existing if (m := re.match(r"^UF-(\d+)$", str(f.get("id", ""))))]
    entry["id"] = f"UF-{max(numbers, default=0) + 1}"
    try:
        source_ids = {s.id for s in guidecfg.load_baseline(root).sources}
    except guidecfg.ConfigError:
        source_ids = None
    problems = guidecfg.check_finding(entry, sources=source_ids)
    if problems:
        raise FindingError("invalid finding: " + "; ".join(problems))
    ordered = {k: entry[k] for k in ("id", "source", "kind", "locations", "summary", "evidence", "status")
               if k in entry}
    ordered.update({k: v for k, v in entry.items() if k not in ordered})
    path = root / "findings" / "upstream.yaml"
    path.parent.mkdir(parents=True, exist_ok=True)
    data = {"findings": existing + [ordered]}
    path.write_text("# Upstream findings collected by cc-guide runs; file them with /guide:upstream.\n"
                    + yaml.safe_dump(data, sort_keys=False, allow_unicode=True))
    return entry["id"]


# --- CLI ---------------------------------------------------------------------------


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Run summaries, upstream findings, and resume points")
    sub = parser.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("summary")
    s.add_argument("--root", type=Path, default=Path("."))
    s.add_argument("--workspace", type=Path, required=True)
    s.add_argument("--run-id", required=True)
    s.add_argument("--command", default="create")
    s.add_argument("--rigor")
    s.add_argument("--model")
    s.add_argument("--effort")
    s.add_argument("--baseline-before")
    s.add_argument("--baseline-after")

    f = sub.add_parser("findings-add")
    f.add_argument("--root", type=Path, default=Path("."))
    f.add_argument("--json")
    f.add_argument("--source")
    f.add_argument("--kind")
    f.add_argument("--location", action="append", default=[])
    f.add_argument("--quote", action="append", default=[])
    f.add_argument("--summary")
    f.add_argument("--evidence")

    r = sub.add_parser("resume")
    r.add_argument("--workspace", type=Path, required=True)
    r.add_argument("--plan", type=Path)
    r.add_argument("--root", type=Path, default=Path("."))

    i = sub.add_parser("run-id", help="print the next run id for a command")
    i.add_argument("--command", required=True)
    i.add_argument("--root", type=Path, default=Path("."))

    args = parser.parse_args(argv)

    if args.cmd == "run-id":
        print(next_run_id(args.root, args.command))
        return 0

    if args.cmd == "summary":
        out = summary(args.root, args.workspace, args.run_id, command=args.command, rigor=args.rigor,
                      model=args.model, effort=args.effort, baseline_before=args.baseline_before,
                      baseline_after=args.baseline_after)
        print(f"wrote {out}")
        return 0

    if args.cmd == "findings-add":
        if args.json:
            raw = sys.stdin.read() if args.json == "-" else Path(args.json).read_text()
            finding = json.loads(raw)
        else:
            if not args.location:
                parser.error("findings-add needs --json or --location")
            locations = []
            for i, ref in enumerate(args.location):
                loc = {"ref": ref}
                if i < len(args.quote):
                    loc["quote"] = args.quote[i]
                locations.append(loc)
            finding = {"source": args.source, "kind": args.kind, "locations": locations,
                       "summary": args.summary, "evidence": args.evidence}
            finding = {k: v for k, v in finding.items() if v is not None}
        try:
            print(findings_add(args.root, finding))
        except FindingError as err:
            print(err, file=sys.stderr)
            return 1
        return 0

    led = parse_ledger(args.workspace / "progress.md")
    total = None
    plan = args.plan
    # The plan comes from --plan, the ledger's identity line, or the plan-path marker
    # that superpowers' sdd-workspace writes; without it a run interrupted before its
    # first ledger line would look finished.
    named = led.plan
    marker = args.workspace / "plan-path"
    if named is None and marker.is_file():
        named = marker.read_text().strip() or None
    if plan is None and named:
        candidate = Path(named)
        plan = candidate if candidate.is_absolute() else args.root / candidate
    if plan is not None and Path(plan).is_file():
        total = plan_task_count(plan)
    point = resume_point(led, total)
    print("done" if point is None else point)
    return 0


if __name__ == "__main__":
    sys.exit(main())
