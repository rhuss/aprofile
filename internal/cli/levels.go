package cli

import (
	"fmt"
	"strings"

	"github.com/cc-deck/aprofile/internal/profile"
)

// This file is the single source of the share-level text (plan Design Decision 6):
// flag help, the TUI, show, `help sharing` and the README tables all render these
// values, and readme_test.go checks the README against them (SC-003).

// levelDescriptions are the one-line descriptions of FR-014 (at most 80 characters,
// used verbatim everywhere a level is shown).
var levelDescriptions = map[profile.Share]string{
	profile.ShareAll:     "Uses your whole setup live; logins stay private, history too unless grouped.",
	profile.ShareContent: "Uses your instructions and skills; keeps its own settings and hooks.",
	profile.ShareNone:    "Shares nothing with your setup; starts clean, with only named hook sets.",
}

// alwaysPrivate is the sentence every guide states up front (FR-012, US2 AS3).
const alwaysPrivate = "logins are always private; history and memory are private unless you put profiles in a history group"

// levelDescription returns the one-line description of a level.
func levelDescription(s profile.Share) string { return levelDescriptions[s] }

// useCase is one row of the use-case table (spec, "Use cases and the recommended
// share level").
type useCase struct {
	ID          string
	Case        string
	Recommended string
	Why         string
}

var useCases = []useCase{
	{"UC1", "Work and personal accounts of the same harness, often running at the same time", "content",
		"Same instructions everywhere; a permission granted at work stays out of the personal account"},
	{"UC2", "A client account that must never see personal memory, instructions or plugins", "none",
		"Nothing is shared; show and doctor list the remaining $HOME escapes"},
	{"UC3", "A throwaway profile for trying a plugin, hook or model endpoint", "content",
		"Plugin and settings experiments stay in the profile; remove --purge throws it away"},
	{"UC4", "Claude Code and Codex used together with a common set of hooks", "content",
		"Use the same hook sets in each profile; content stays per harness"},
	{"UC5", "A local or alternative endpoint that should behave exactly like the default setup", "all",
		"Settings, plugins and hooks stay identical to the default setup and change with it"},
	{"UC6", "A demo or recording profile with nothing personal on screen", "none",
		"Personal instructions, permissions and plugins stay out of view"},
	{"UC7", "A CI or automation profile with only explicitly chosen hooks", "none",
		"No borrowed settings; only the hook sets the profile names (--hook-sets)"},
}

// historyUseCases are the history group use cases of spec 003 (FR-026).
var historyUseCases = []useCase{
	{"H1", "Personal aliases (other endpoints or accounts) continue each other's conversations and those of the plain default setup", "--group home",
		"Each alias links its history to the default setup; logins, endpoints and models stay per profile"},
	{"H2", "Work profiles continue each other's conversations, never personal ones", "--group work",
		"aprofile group create claude/work first; work history lives apart from the default setup"},
	{"H3", "A client profile that must never see other history or memory, and whose history no other profile sees", "no group",
		"The profile's history is its own (isolated), as without history groups"},
	{"H4", "A demo profile with a clean history", "no group",
		"Nothing from other conversations shows up"},
}

// levelRow is one row of the level table: what each level does with a category.
type levelRow struct {
	Category string
	All      string
	Content  string
	None     string
}

var levelTable = []levelRow{
	{"Credentials", "private", "private", "private"},
	{"History (conversations, prompt history, project memory)", "private, or shared in a history group", "private, or shared in a history group", "private, or shared in a history group"},
	{"Other runtime state (logs, caches, per-process files)", "private", "private", "private"},
	{"Owned (MCP servers, model, endpoint)", "private", "private", "private, starts empty"},
	{"Content (instructions, skills, agents, commands, prompts)", "linked", "linked", "not present"},
	{"Settings (permissions, enabled plugins, status line)", "linked (no feature entries: features need content or none)", "own copy, taken once", "own, starts minimal"},
	{"Plugin store (installed plugin code)", "linked", "linked", "private"},
	{"Hooks", "the default setup's", "hook sets (named + every-profile)", "named hook sets only"},
}

// shareFlagHelp is the help text of the --share flag.
func shareFlagHelp() string {
	var parts []string
	for _, s := range profile.Shares {
		parts = append(parts, fmt.Sprintf("%s: %s", s, levelDescription(s)))
	}
	return "share level (all|content|none; see: aprofile help sharing). " + strings.Join(parts, " ")
}

// useCasesFor returns the use cases a level is recommended for.
func useCasesFor(s profile.Share) []useCase {
	var out []useCase
	for _, uc := range useCases {
		if uc.Recommended == string(s) {
			out = append(out, uc)
		}
	}
	return out
}
