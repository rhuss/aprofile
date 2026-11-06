"""Baseline values and experimental labels for the MkDocs adapter (mkdocs-macros-plugin).

mkdocs.yml loads this file as the macros module (module_name: tools/guide/mkdocs_macros).
The adapter uses non-default Jinja delimiters so that Go, Helm, Ansible, and shell
syntax in listings ({{ .Values.x }}, {% if %}, ${#array[@]}) never reaches Jinja:

  {{= attr("mini-commit-short") }}                         a baseline value
  {{= experimental("in-review", "Status history", 4) }}    an experimental label
  {{= baseline_note() }}                                   the baseline note

attr() fails the build on an unknown name, so a missing baseline value cannot slip
through (FR-018). The values come from tools/guide/attrs.py, the same code the other
adapters use. define_env also stops the build when guide.yaml has a branding block,
which the MkDocs adapter does not support (tools/guide/branding.py); every `mkdocs
build` (site, site-internal, check, preview) loads this module.
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import attrs  # noqa: E402
import branding  # noqa: E402
import guidecfg  # noqa: E402

TIERS = {
    "in-review": "Experimental: in review",
    "on-main": "Experimental: on main",
}


def label(tier: str, feature: str, pr: int | str, baseline: guidecfg.Baseline, captured: str) -> str:
    """Markdown for an experimental admonition (contracts/page-conventions.md, section 4)."""
    if tier not in TIERS:
        raise ValueError(f"unknown experimental tier {tier!r} (expected in-review or on-main)")
    cr = baseline.change_request(int(pr))
    ref = f"change request #{pr}"
    if cr is not None:
        src = baseline.source(cr.source)
        if src is not None and src.web_url.startswith(("http://", "https://")):
            ref = f"[change request #{pr}]({src.web_url}/pull/{pr})"
    if tier == "in-review":
        body = (f"{feature} is not part of the baseline. It is proposed in {ref} (status as of "
                f"{captured}) and may change or be dropped before it merges.")
    else:
        body = (f"{feature} is an experimental feature. It is merged on the default branch "
                f"({ref}) but not part of the pinned release, and its behavior may change.")
    return f'!!! experimental "{TIERS[tier]}"\n\n    {body}\n'


def define_env(env) -> None:
    root = Path(env.project_dir)
    problems, _ = branding.check_guide(root)
    if problems:
        raise ValueError("guide.yaml branding problems:\n" + "\n".join(problems))
    baseline = guidecfg.load_baseline(root)
    values = attrs.values(baseline)

    @env.macro
    def attr(name: str) -> str:
        """A baseline value by attribute name (for example mini-commit-short)."""
        if name not in values:
            raise KeyError(f"unknown baseline attribute {name!r}; known: {', '.join(sorted(values))}")
        return values[name]

    @env.macro
    def experimental(tier: str, feature: str, pr: int | str) -> str:
        """An experimental label for an in-review or on-main change request."""
        return label(tier, feature, pr, baseline, baseline.captured)

    @env.macro
    def baseline_note() -> str:
        """The baseline note: which source revisions the guide was verified against."""
        return ('!!! note "Baseline"\n\n'
                f"    This guide was verified against {values['baseline-sources']} "
                f"(captured {values['baseline-date']}).\n")

    env.variables["baseline_values"] = values
