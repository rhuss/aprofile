package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/config"
	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
)

// newSharingHelpCmd registers the `aprofile help sharing` topic (FR-013): a command
// without Run, which cobra lists under "Additional help topics". Its help prints
// the decision guide from the single text source (levels.go).
func newSharingHelpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sharing",
		Short: "How share levels work and which one fits your use case",
	}
	cmd.SetHelpFunc(func(c *cobra.Command, _ []string) {
		set, err := harness.Load(harnessesDirOrEmpty())
		if err != nil {
			set = nil
		}
		writeSharingGuide(c.OutOrStdout(), set)
	})
	return cmd
}

// harnessesDirOrEmpty returns the harnesses.d dir, or "" when paths cannot be
// resolved (the guide then shows the built-ins).
func harnessesDirOrEmpty() string {
	if p, err := config.ResolvePaths(); err == nil {
		return p.HarnessesDir()
	}
	return ""
}

// writeSharingGuide renders the guide: the always-private sentence first, the
// levels, the use-case table, the level table and per-harness differences.
func writeSharingGuide(w io.Writer, set *harness.Set) {
	fmt.Fprintln(w, "Share levels: how much of your default setup a profile uses")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "At every level, %s.\n", alwaysPrivate)
	fmt.Fprintln(w, "Logs and caches are always private too. The level only decides how much of your setup a profile uses.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Levels (choose with --share on add, edit, copy and import):")
	for _, s := range profile.Shares {
		fmt.Fprintf(w, "  %-8s %s\n", s, levelDescription(s))
	}
	fmt.Fprintln(w, "  New profiles get content unless settings.yaml sets default_share.")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Which level fits your use case:")
	uc := [][]string{{"#", "Use case", "Level", "Why"}}
	for _, u := range useCases {
		uc = append(uc, []string{u.ID, u.Case, u.Recommended, u.Why})
	}
	writeTable(w, uc, []int{4, 46, 8, 0})
	fmt.Fprintln(w)

	fmt.Fprintln(w, "What each level does:")
	lt := [][]string{{"Category", "all", "content", "none"}}
	for _, r := range levelTable {
		lt = append(lt, []string{r.Category, r.All, r.Content, r.None})
	}
	writeTable(w, lt, []int{36, 20, 22, 0})
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A none profile starts with the harness's first-run onboarding; that is expected.")
	fmt.Fprintln(w, "Preview a change with: aprofile edit <harness>/<name> --share <level> --dry-run")
	fmt.Fprintln(w, "See one profile's entries with: aprofile show <harness>/<name>")

	writeHistoryGroupsGuide(w, set)

	if set == nil {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Per-harness differences (aprofile harness show <name> has the details):")
	for _, d := range set.All() {
		diffs := harnessDifferences(d)
		if len(diffs) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s:\n", d.Name)
		for _, line := range diffs {
			fmt.Fprintf(w, "    - %s\n", line)
		}
	}
}

// harnessDifferences lists how one definition differs from the level table
// (FR-022): settings files that are never linked (FR-009a), where hooks live, a
// missing plugin store, $HOME escapes and policy cells it overrides.
func harnessDifferences(d *harness.Definition) []string {
	var out []string
	hasPluginStore := false
	for _, r := range d.Classification {
		if r.IsGlob() || r.CompanionOf != "" {
			continue
		}
		switch r.Layer {
		case harness.LayerPluginStore:
			hasPluginStore = true
		case harness.LayerMixed:
			if owned := ownedKeys(r); len(owned) > 0 {
				out = append(out, fmt.Sprintf("%s is never linked, at any level: it holds %s (owned keys); at all and content it starts as a copy of the default setup's (credentials and state keys left out), at none it starts minimal", r.Path(), strings.Join(owned, ", ")))
			}
		case harness.LayerOwned:
			if d.MCP.File() != "" && d.MCP.File() == r.Path() {
				out = append(out, fmt.Sprintf("%s is private at every level, all included: it holds the MCP servers; at all and content it starts as a copy of the default setup's, at none it starts absent", r.Path()))
			}
		}
	}
	if d.Hooks != nil {
		switch {
		case d.Hooks.Form == harness.HookFormFiles:
			out = append(out, fmt.Sprintf("hooks are files in %s/: linked at all; at content and none only hook-set files are linked there", d.Hooks.File()))
		case d.HookFileRule() != nil:
			out = append(out, fmt.Sprintf("hooks are the %q key of %s: at all they come with the linked file; at content and none hook sets are placed there", d.Hooks.Key(), d.Hooks.File()))
		default:
			out = append(out, fmt.Sprintf("hooks live in %s, a separate file: linked at all; the profile's own at content and none", d.Hooks.File()))
		}
	} else {
		out = append(out, "no hook location is declared: hook sets are not supported")
	}
	if !hasPluginStore {
		out = append(out, "no separate plugin store (extensions are single files)")
	}
	for _, he := range d.HomeEscapes {
		out = append(out, fmt.Sprintf("%s (%s) is read from $HOME: shared with every profile; no share level can separate it", he.Path, he.What))
	}
	var cats []string
	for cat := range d.SharePolicy {
		cats = append(cats, string(cat))
	}
	sortStrings(cats)
	for _, c := range cats {
		cat := harness.Layer(c)
		for _, lv := range harness.Levels {
			if t := d.Treatment(cat, lv); t != harness.DefaultSharePolicy[cat].Get(lv) {
				out = append(out, fmt.Sprintf("%s at %s: %s (instead of %s)", cat, lv, t, harness.DefaultSharePolicy[cat].Get(lv)))
			}
		}
	}
	return out
}

func ownedKeys(r harness.EntryRule) []string {
	var out []string
	for k, l := range r.Keys {
		if l == harness.LayerOwned {
			out = append(out, k)
		}
	}
	sortStrings(out)
	return out
}

// writeTable prints rows as aligned columns; a width of 0 means "rest of line".
// Long cells wrap within their column.
func writeTable(w io.Writer, rows [][]string, widths []int) {
	for i, row := range rows {
		cells := make([][]string, len(row))
		lines := 1
		for c, cell := range row {
			width := widths[c]
			if width == 0 {
				width = 60
			}
			cells[c] = wrapText(cell, width-1)
			if len(cells[c]) > lines {
				lines = len(cells[c])
			}
		}
		for l := 0; l < lines; l++ {
			var b strings.Builder
			b.WriteString("  ")
			for c := range row {
				text := ""
				if l < len(cells[c]) {
					text = cells[c][l]
				}
				if widths[c] == 0 || c == len(row)-1 {
					b.WriteString(text)
				} else {
					b.WriteString(fmt.Sprintf("%-*s", widths[c], text))
				}
			}
			fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
		}
		if i == 0 {
			fmt.Fprintln(w, "  "+strings.Repeat("-", 100))
		}
	}
}

// wrapText splits text into lines of at most width characters at spaces.
func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, word := range words[1:] {
		if len(cur)+1+len(word) > width {
			lines = append(lines, cur)
			cur = word
			continue
		}
		cur += " " + word
	}
	return append(lines, cur)
}

// writeHistoryGroupsGuide renders the "History groups" section (spec 003, FR-023,
// FR-026): home, own groups, isolated profiles, the use cases, the concurrent-use
// warning, account visibility, and per harness the support and history_note.
func writeHistoryGroupsGuide(w io.Writer, set *harness.Set) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "History groups: which profiles share conversation history")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A history group is a set of profiles of one harness that share one conversation history:")
	fmt.Fprintln(w, "a conversation started in one member can be resumed in another with the harness's own resume,")
	fmt.Fprintln(w, "and it then runs with that member's login, endpoint and model. There is one copy of it on disk.")
	fmt.Fprintln(w, "  home       built in for every harness that supports groups; its store is your default setup,")
	fmt.Fprintln(w, "             so members share history with plain `claude` (or codex, pi). Join: --group home")
	fmt.Fprintln(w, "  own groups made with: aprofile group create <harness>/<group>; history lives in aprofile's")
	fmt.Fprintln(w, "             data directory, apart from the default setup. Join: --group <group>")
	fmt.Fprintln(w, "  no group   (isolated) the default: the profile's history is its own")
	fmt.Fprintln(w, "Logins, settings and all other state stay private in every group. Membership does not depend on")
	fmt.Fprintln(w, "the share level: a none profile can join home and then shares only history.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Which group fits your use case:")
	uc := [][]string{{"#", "Use case", "Group", "Why"}}
	for _, u := range historyUseCases {
		uc = append(uc, []string{u.ID, u.Case, u.Recommended, u.Why})
	}
	writeTable(w, uc, []int{4, 46, 14, 0})
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Join or leave later with: aprofile edit <harness>/<name> --group <group> | --no-group (--dry-run first).")
	fmt.Fprintln(w, "A join moves your existing conversations into the group where nothing is in the way and sets the")
	fmt.Fprintln(w, "rest aside in aprofile's data directory; nothing is deleted. Leaving gives fresh, empty history.")
	fmt.Fprintln(w, "Running the same conversation in two profiles at the same time is not prevented and can interleave")
	fmt.Fprintln(w, "its transcript: resume a conversation in one profile at a time.")
	fmt.Fprintln(w, "Shared conversations show which account wrote them: transcripts carry account identifiers.")
	if set == nil {
		return
	}
	fmt.Fprintln(w, "Per harness:")
	for _, d := range set.All() {
		if !d.SupportsGroups() {
			fmt.Fprintf(w, "  %s: history groups not supported (its definition declares no history entries)\n", d.Name)
			continue
		}
		var entries []string
		for _, r := range d.HistoryRules() {
			if r.CompanionOf == "" {
				entries = append(entries, r.Path())
			}
		}
		fmt.Fprintf(w, "  %s: history entries %s\n", d.Name, strings.Join(entries, ", "))
		if d.HistoryNote != "" {
			fmt.Fprintf(w, "    note: %s\n", d.HistoryNote)
		}
	}
}
