package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/history"
	"github.com/cc-deck/aprofile/internal/layer"
	"github.com/cc-deck/aprofile/internal/profile"
)

// concurrentUseNotice is printed once whenever a profile joins a history group
// (FR-023).
const concurrentUseNotice = "note: running the same conversation in two profiles at the same time is not prevented and can interleave its transcript; resume a conversation in one profile at a time"

// groupFlagHelp is the help text of --group.
const groupFlagHelp = "history group of the profile's harness to share conversation history with: home (the default setup) or a group made with aprofile group create; without it the profile's history is its own"

// groupExitErr maps an "operation not valid" history error to exit 2.
func groupExitErr(err error) error {
	if err == nil {
		return nil
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return err
	}
	if history.IsInvalid(err) {
		return &ExitError{Code: ExitInvalidForLevel, Err: err}
	}
	return err
}

// profileConfigDirs lists the config dirs of every profile of a harness.
func (a *app) profileConfigDirs(harnessName string) []string {
	profiles, _ := a.store.ListByHarness(harnessName)
	var out []string
	for _, p := range profiles {
		out = append(out, a.paths.ProfileConfigDir(p.Harness, p.Name))
	}
	return out
}

// checkGroupJoin validates that a profile of def may join group: the group exists
// (FR-009, listing the harness's groups otherwise) and, for home, the default
// location can serve as the store (Edge Cases). Every refusal is exit 2 and
// changes nothing.
func (a *app) checkGroupJoin(def *harness.Definition, group string) (*history.Group, error) {
	g, err := a.groups().Get(def.Name, group)
	if err != nil {
		return nil, groupExitErr(err)
	}
	if g.Builtin {
		if err := history.CheckHomeLocation(def, a.paths.Home, a.paths, a.profileConfigDirs(def.Name)); err != nil {
			return nil, groupExitErr(err)
		}
	}
	return g, nil
}

// joinNewProfile prepares the config dir of a profile that was just created in a
// history group: its history entries are linked to the store and, for home, a
// missing history directory is created empty in the default location (an explicit
// join, FR-016). A new profile has no history, so nothing moves.
func (a *app) joinNewProfile(def *harness.Definition, p *profile.Profile) error {
	st, err := a.groups().StoreFor(def, p)
	if err != nil {
		return groupExitErr(err)
	}
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	if _, err := layer.PrepareConfigDirWith(def, p, configDir, layer.PrepareOptions{HistoryStore: st, ExplicitJoin: true}); err != nil {
		return fmt.Errorf("prepare config dir of %s: %w", p.ID(), err)
	}
	fmt.Fprintf(a.out, "  history group: %s (store %s)\n", p.HistoryGroup, st.Root())
	fmt.Fprintln(a.out, "  "+concurrentUseNotice)
	return nil
}

// historyLine describes a profile's history for show (FR-020).
func historyLine(p *profile.Profile) string {
	switch {
	case p.HistoryGroup == "":
		return "private (isolated: in no history group)"
	case p.HistoryGroup == history.Home && p.ShareLevel() == profile.ShareNone:
		return "shares history with your default setup (history group home), although it borrows nothing else at share level none"
	case p.HistoryGroup == history.Home:
		return "shares history with your default setup and the other members of history group home"
	default:
		return "shares history with the members of history group " + p.HistoryGroup
	}
}

// joinNames joins names with ", " or returns none.
func joinNames(names []string, none string) string {
	if len(names) == 0 {
		return none
	}
	return strings.Join(names, ", ")
}

// historyPlanOptions returns the plan options that carry a profile's history store
// (nil when isolated; history entries skipped for a group that does not exist).
func (a *app) historyPlanOptions(def *harness.Definition, p *profile.Profile) layer.PlanOptions {
	po := layer.PlanOptions{ProfileID: p.ID()}
	prep := a.prepareOptions(def, p)
	po.HistoryStore, po.SkipHistory = prep.HistoryStore, prep.SkipHistory
	return po
}

// changeGroup applies a membership change as one operation (FR-012 to FR-015,
// data-model.md transitions): to is the target group ("" leaves the group). It
// plans with internal/history, prints the plan, stops on --dry-run, refuses while
// the profile runs (exit 4), asks once when anything moves or is set aside (exit 3
// off a terminal without --yes), applies exactly the listed steps and records the
// group on p. runningChecked is set when the same edit already checked that the
// profile is not running (a share change before it). A join records the new group
// before it applies anything, so a join that fails part way is reported by sync
// and doctor as member drift with the command that completes it. It returns
// done=false when nothing should be saved.
func (a *app) changeGroup(cmd *cobra.Command, p *profile.Profile, def *harness.Definition, to string, dryRun, yes, runningChecked bool) (bool, error) {
	from := p.HistoryGroup
	configDir := a.paths.ProfileConfigDir(p.Harness, p.Name)
	now := time.Now()
	setAside := history.SetAsideRunDir(a.paths, p.Harness, p.Name, now)
	name := func(g string) string {
		if g == "" {
			return "isolated"
		}
		return g
	}
	if to == "" && from == "" {
		fmt.Fprintf(a.out, "nothing to do: %s is in no history group\n", p.ID())
		return true, nil
	}
	var steps []layer.Step
	var err error
	header := fmt.Sprintf("history group change for %s: %s -> %s", p.ID(), name(from), name(to))
	if to == "" {
		steps, err = history.PlanLeave(def, configDir, a.groups().StoreOf(def, from))
		header += " (the group's history is not changed):"
	} else {
		if !def.SupportsGroups() {
			return false, groupExitErr(fmt.Errorf("harness %s does not support history groups: %w", def.Name, history.ErrNotSupported))
		}
		st := a.groups().StoreOf(def, to)
		if from != "" {
			// Links of entries that are no longer history into the current store
			// go first (FR-022a), on a move and on a re-join alike.
			steps = history.PlanDetach(def, configDir, a.groups().StoreOf(def, from))
		}
		var join []layer.Step
		join, err = history.PlanJoin(def, st, configDir, setAside)
		steps = append(steps, join...)
		header += fmt.Sprintf(" (store %s):", st.Root())
	}
	if err != nil {
		return false, groupExitErr(err)
	}
	if len(steps) == 0 {
		// A membership-only change is still refused while the profile runs (FR-013),
		// before anything that reads like a success is printed.
		if !dryRun && to != from {
			if ok, err := a.checkNotRunning(cmd, p, configDir, yes, runningChecked); !ok || err != nil {
				return false, err
			}
		}
		switch {
		case to == from:
			fmt.Fprintf(a.out, "nothing to do: %s is in history group %s and all its history entries are linked\n", p.ID(), name(to))
		case to == "":
			// No history link to remove (for example a group that does not exist,
			// whose entries sync left private): only the membership changes.
			fmt.Fprintf(a.out, "%s leaves history group %s; it has no history links to remove\n", p.ID(), from)
		default:
			fmt.Fprintf(a.out, "%s joins history group %s; its history entries are linked already\n", p.ID(), to)
			if !dryRun {
				fmt.Fprintln(a.out, "  "+concurrentUseNotice)
			}
		}
		if dryRun {
			return false, nil
		}
		if to == from {
			return true, nil // nothing changes; other edited fields are still saved
		}
		p.HistoryGroup = to
		return true, nil
	}
	if dryRun {
		header = "(dry-run) " + header
	}
	fmt.Fprintln(a.out, header)
	printPlan(a, steps)
	if history.NeedsConfirmation(steps) {
		fmt.Fprintf(a.out, "history moved or set aside: yes; set-aside history goes to %s\n", setAside)
	} else {
		fmt.Fprintln(a.out, "history moved or set aside: no; nothing is deleted")
	}
	if dryRun {
		return false, nil
	}

	if ok, err := a.checkNotRunning(cmd, p, configDir, yes, runningChecked); !ok || err != nil {
		return false, err
	}
	if history.NeedsConfirmation(steps) {
		err := confirmOrYes(cmd, fmt.Sprintf("Move the listed history of %s into the store and set the rest aside?", p.ID()), yes)
		if err == errDeclined {
			fmt.Fprintln(a.out, "aborted; nothing changed")
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	if to != "" {
		// Recorded first: should the join fail part way, the profile is a member
		// whose remaining entries sync and doctor report with the command that
		// completes the join, instead of an isolated profile with links into a store.
		p.HistoryGroup = to
		if err := a.store.Save(p); err != nil {
			return false, err
		}
	}
	applied, _, err := layer.Apply(steps, layer.ApplyOptions{Yes: true,
		SetAsideManifest: history.ManifestWriter(setAside, p.ID(), to, now)})
	if err != nil {
		// Report item by item what was applied before the failure (FR-014).
		for _, s := range applied {
			fmt.Fprintf(a.errOut, "aprofile: applied before the failure: %s\n", s.String())
		}
		hint := "aprofile edit " + p.ID() + " --group " + to
		if to == "" {
			hint = "aprofile edit " + p.ID() + " --no-group"
		}
		return false, fmt.Errorf("%w (re-run: %s)", err, hint)
	}
	for i := range applied {
		if i < len(steps) && applied[i].Kind != steps[i].Kind {
			fmt.Fprintf(a.errOut, "aprofile: note: %s\n", applied[i].String())
		}
	}
	p.HistoryGroup = to
	if to == "" {
		fmt.Fprintf(a.out, "left history group %s; the profile's history is its own again\n", from)
	} else {
		fmt.Fprintf(a.out, "joined history group %s\n", to)
		if from != to {
			fmt.Fprintln(a.out, "  "+concurrentUseNotice)
		}
	}
	return true, nil
}

// confirmGroupFirst resolves the confirmation of a membership change before any
// other change of the same edit is applied (FR-013: exit 3 changes nothing). It
// returns yes=true when the change needs no confirmation or was confirmed, so
// changeGroup does not ask again; ok=false when the user declined.
func (a *app) confirmGroupFirst(cmd *cobra.Command, p *profile.Profile, def *harness.Definition, to string, yes bool) (confirmed, ok bool, err error) {
	if to == "" {
		return yes, true, nil
	}
	// Planned even with --yes: a join the planner refuses must fail before the
	// share change writes anything.
	steps, err := history.PlanJoin(def, a.groups().StoreOf(def, to), a.paths.ProfileConfigDir(p.Harness, p.Name),
		history.SetAsideRunDir(a.paths, p.Harness, p.Name, time.Now()))
	if err != nil {
		return false, false, groupExitErr(err)
	}
	if yes {
		return yes, true, nil
	}
	if !history.NeedsConfirmation(steps) {
		return false, true, nil
	}
	err = confirmOrYes(cmd, fmt.Sprintf("Joining history group %s moves or sets aside history of %s; continue?", to, p.ID()), false)
	if err == errDeclined {
		fmt.Fprintln(a.out, "aborted; nothing changed")
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, true, nil
}

// checkNotRunning refuses a history group change while the profile runs (exit 4,
// FR-013). When the running state cannot be determined it asks first (or needs
// --yes off a terminal). It returns ok=false without an error when the user
// declined. runningChecked skips the check when the same edit already made it.
func (a *app) checkNotRunning(cmd *cobra.Command, p *profile.Profile, configDir string, yes, runningChecked bool) (bool, error) {
	if runningChecked {
		return true, nil
	}
	proc, running, known := profileRunning(configDir)
	if running {
		return false, exitErrorf(ExitProfileRunning, "profile %s is in use by a running process (pid %d %s); stop it before changing its history group", p.ID(), proc.PID, proc.Name)
	}
	if !known {
		err := confirmOrYes(cmd, fmt.Sprintf("cannot tell whether profile %s is running; continue only if it is stopped?", p.ID()), yes)
		if err == errDeclined {
			fmt.Fprintln(a.out, "aborted; nothing changed")
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	return true, nil
}
