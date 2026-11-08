package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cc-deck/aprofile/internal/history"
)

// groupJSON is one history group in --json output (data-model.md "JSON output").
type groupJSON struct {
	Harness            string           `json:"harness"`
	Name               string           `json:"name"`
	Builtin            bool             `json:"builtin"`
	Store              string           `json:"store"`
	Members            []string         `json:"members"`
	DefaultSetupMember bool             `json:"default_setup_member"`
	Entries            []groupEntryJSON `json:"entries,omitempty"`
}

// groupEntryJSON is one history entry of a group store (group show).
type groupEntryJSON struct {
	Entry     string `json:"entry"`
	Merge     string `json:"merge"`
	SizeBytes int64  `json:"size_bytes"`
	Exists    bool   `json:"exists"`
}

func toGroupJSON(g *history.Group) groupJSON {
	members := g.Members
	if members == nil {
		members = []string{}
	}
	return groupJSON{Harness: g.Harness, Name: g.Name, Builtin: g.Builtin, Store: g.Store, Members: members, DefaultSetupMember: g.DefaultSetupMember}
}

// parseGroupRef parses "<harness>/<group>". A malformed reference or an invalid
// group name is an "operation not valid" error (exit 2): nothing is created.
func parseGroupRef(s string) (harnessName, group string, err error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], "/") {
		return "", "", exitErrorf(ExitInvalidForLevel, "history group reference %q must be <harness>/<group>", s)
	}
	return parts[0], parts[1], nil
}

func newGroupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "group",
		Short: "Manage history groups: profiles of one harness that share conversation history",
		Long: "A history group is a named set of profiles of one harness that share one conversation history.\n" +
			"Every harness whose definition declares history entries has the built-in group home, whose store is\n" +
			"the default setup. Other groups are created explicitly and keep their history in aprofile's data\n" +
			"directory. A profile in no group keeps its history to itself. Logins always stay private.\n" +
			"See: aprofile help sharing",
	}
	cmd.AddCommand(newGroupListCmd(), newGroupShowCmd(), newGroupCreateCmd(), newGroupDeleteCmd(), newGroupRenameCmd())
	// An unknown subcommand is an error (exit 1, like other cobra usage errors),
	// not the help text with exit 0; rename has its own command with exit 2.
	cmd.Args = cobra.NoArgs
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return cmd.Help() }
	return cmd
}

// newGroupRenameCmd exists only to answer a rename attempt clearly (US4): groups
// cannot be renamed, and home is built in. Members move with edit --group.
func newGroupRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "rename <harness>/<group> <new-name>",
		Short:  "Groups cannot be renamed; move members with: aprofile edit <profile> --group <group>",
		Hidden: true,
		Args:   cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				if h, g, err := parseGroupRef(args[0]); err == nil && g == history.Home {
					return exitErrorf(ExitInvalidForLevel, "%s/home is built in: it always exists and cannot be created, renamed or deleted", h)
				}
			}
			return exitErrorf(ExitInvalidForLevel, "history groups cannot be renamed; create the new group, move each member with: aprofile edit <harness>/<name> --group <new>, then delete the old group")
		},
	}
}

func newGroupCreateCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "create <harness>/<group>",
		Short: "Create a history group and its empty store",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			h, g, err := parseGroupRef(args[0])
			if err != nil {
				return err
			}
			grp, err := a.groups().Create(h, g, time.Now())
			if err != nil {
				return groupExitErr(err)
			}
			if asJSON {
				return writeJSON(a.out, toGroupJSON(grp))
			}
			fmt.Fprintf(a.out, "created history group %s (store %s)\n", grp.ID(), grp.Store)
			fmt.Fprintf(a.out, "  add profiles with: aprofile add %s <name> --group %s, or aprofile edit %s/<name> --group %s\n", h, g, h, g)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

// membersText renders the members of a group for text output.
func membersText(g *history.Group, countOnly bool) string {
	var parts []string
	if g.DefaultSetupMember {
		parts = append(parts, "default setup")
	}
	if countOnly {
		if len(g.Members) > 0 || len(parts) == 0 {
			parts = append(parts, strconv.Itoa(len(g.Members)))
		}
	} else {
		parts = append(parts, g.Members...)
	}
	return joinNames(parts, "none")
}

func newGroupListCmd() *cobra.Command {
	var asJSON bool
	var harnessName string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the history groups of every harness that supports them (home first)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			groups, err := a.groups().List(harnessName)
			if err != nil {
				return groupExitErr(err)
			}
			if asJSON {
				out := []groupJSON{}
				for _, g := range groups {
					out = append(out, toGroupJSON(g))
				}
				return writeJSON(a.out, out)
			}
			tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "HARNESS\tGROUP\tMEMBERS\tSTORE")
			for _, g := range groups {
				name := g.Name
				if g.Builtin {
					name += " (built in)"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", g.Harness, name, membersText(g, true), g.Store)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	cmd.Flags().StringVar(&harnessName, "harness", "", "only the groups of this harness")
	return cmd
}

// entrySize returns whether a store entry exists and the bytes of its files.
func entrySize(path string) (bool, int64) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, 0
	}
	var size int64
	_ = filepath.Walk(resolved, func(_ string, fi os.FileInfo, err error) error {
		if err == nil && fi.Mode().IsRegular() {
			size += fi.Size()
		}
		return nil
	})
	return true, size
}

func newGroupShowCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show <harness>/<group>",
		Short: "Show a history group: store, members and history entries",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			h, name, err := parseGroupRef(args[0])
			if err != nil {
				return err
			}
			g, err := a.groups().Get(h, name)
			if err != nil {
				return groupExitErr(err)
			}
			def, _ := a.set.Get(h)
			st := a.groups().StoreOf(def, name)
			gj := toGroupJSON(g)
			gj.Entries = []groupEntryJSON{}
			for _, r := range def.HistoryRules() {
				exists, size := entrySize(st.EntryPath(r.Path()))
				gj.Entries = append(gj.Entries, groupEntryJSON{Entry: r.Path(), Merge: string(r.EffectiveMerge()), SizeBytes: size, Exists: exists})
			}
			if asJSON {
				return writeJSON(a.out, gj)
			}
			kind := "created with aprofile group create"
			if g.Builtin {
				kind = "built in: the default setup is its store and its implicit member"
			}
			fmt.Fprintf(a.out, "history group %s (%s)\n", g.ID(), kind)
			fmt.Fprintf(a.out, "  store:    %s\n", g.Store)
			fmt.Fprintf(a.out, "  members:  %s\n", membersText(g, false))
			fmt.Fprintln(a.out, "  history entries:")
			tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
			for _, e := range gj.Entries {
				state := "absent"
				if e.Exists {
					state = fmt.Sprintf("%d bytes", e.SizeBytes)
				}
				fmt.Fprintf(tw, "    %s\tmerge: %s\t%s\n", e.Entry, e.Merge, state)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}

func newGroupDeleteCmd() *cobra.Command {
	var asJSON, purge, yes bool
	cmd := &cobra.Command{
		Use:   "delete <harness>/<group>",
		Short: "Delete a history group without members (its store is kept unless --purge)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := newApp(cmd)
			if err != nil {
				return err
			}
			h, name, err := parseGroupRef(args[0])
			if err != nil {
				return err
			}
			g, err := a.groups().CheckDelete(h, name)
			if err != nil {
				return groupExitErr(err)
			}
			if purge {
				err := confirmOrYes(cmd, fmt.Sprintf("Delete history group %s and its store %s with all its history?", g.ID(), g.Store), yes)
				if err == errDeclined {
					fmt.Fprintln(a.out, "aborted; nothing changed")
					return nil
				}
				if err != nil {
					return err
				}
			}
			kept, err := a.groups().Delete(h, name, purge)
			if err != nil {
				return groupExitErr(err)
			}
			if asJSON {
				return writeJSON(a.out, struct {
					Harness   string `json:"harness"`
					Name      string `json:"name"`
					KeptStore string `json:"kept_store"`
				}{h, name, kept})
			}
			if kept != "" {
				fmt.Fprintf(a.out, "deleted history group %s; its store is kept at %s (move or remove it by hand; it is not adopted by a new group of the same name)\n", g.ID(), kept)
			} else {
				fmt.Fprintf(a.out, "deleted history group %s and its store %s\n", g.ID(), g.Store)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&purge, "purge", false, "also delete the group's store with its history (asks first)")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm --purge (required off a terminal)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON")
	return cmd
}
