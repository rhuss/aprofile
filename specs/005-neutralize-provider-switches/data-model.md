# Data Model: Neutralize Provider Switches

No new stored types. The feature changes which entries the existing placed-entry record holds and
adds one reporting item.

## Desired entries for a profile (changes spec 004 data-model "Desired entries")

Inputs: profile, harness definition, the decided settings (`DecideWith`), and whether the folder
can carry env entries (`envCarrier` returns no reason).

| Desired entry | Before | After |
|---|---|---|
| Folder-carried variables, `APROFILE_SET_VARS` | unchanged | unchanged |
| Empty entry for each auth-clearing name | only with a folder-carried key | whenever the folder can carry env entries |
| Key command / provider `auth` block | unchanged | unchanged |

Excluded from the empty entries (supplied names):

- variables of every setting with `IsVariable()` (model, endpoint, extra and exec-hook variables,
  whichever carrier), as today;
- the variable of every key setting whose carrier is the wrapper (new).

At share level `all` (env file linked) the folder carries no env entries, so the empty entries are
not desired; the record keeps dropping entries for a linked file (spec 004 FR-008a).

## Placed-entry record

Unchanged format (`RecordFormatVersion` stays). Neutralizing entries are `env.<NAME>` entries of
the env file, like the ones spec 004 FR-006a already records for key-carrying folders.

State transitions for one name on the auth-clearing list (profile with its own settings file):

| From | Event | To |
|---|---|---|
| absent | `sync` | placed (empty), recorded |
| placed | user deletes or edits it | stale; next `sync` restores |
| placed | name becomes supplied (extra var, wrapper-carried key) | next `sync` removes it (spec 004 FR-005) |
| placed | share level changes to `all` | record dropped, file replaced by link (spec 004 FR-008a) |
| user's own non-empty entry | first `sync` | replaced by the empty entry, `sync` names it (spec 004 FR-004) |

## Wrapper-only report item (new, reporting only)

`WrapperOnly{Setting: "auth-clearing", Variable: "", Reason: "share-all"}` in
`StatusReport.Folder()`, present when all hold:

- the definition declares `folder_launch.env`;
- the env file is linked (share level `all`);
- the profile's auth kind sets no variables (interactive login);
- the definition's auth-clearing list is not empty.

Its presence makes the folder status `not-self-contained` (spec 004 FR-020 rule: any wrapper-only
item). It never appears in `DecideWith` output, the planner or the wrapper.
