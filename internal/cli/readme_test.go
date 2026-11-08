package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T057/SC-003: the README decision guide carries exactly the tables and level
// descriptions the CLI renders from levels.go, so the README and
// `aprofile help sharing` cannot disagree. Run with APROFILE_PRINT_README=1 to
// print the blocks for pasting.
func TestReadmeMatchesLevels(t *testing.T) {
	blocks := map[string]string{
		"levels":      levelListMarkdown(),
		"use cases":   useCaseMarkdown(),
		"level table": levelTableMarkdown(),
		// Spec 003 (FR-026): the history group use cases.
		"history use cases": historyUseCaseMarkdown(),
	}
	if os.Getenv("APROFILE_PRINT_README") == "1" {
		for _, k := range []string{"levels", "use cases", "level table"} {
			t.Logf("--- %s ---\n%s", k, blocks[k])
		}
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for name, block := range blocks {
		if !strings.Contains(readme, block) {
			t.Errorf("README does not contain the %s block rendered from levels.go:\n%s", name, block)
		}
	}
	guide := readme[strings.Index(readme, "## Choosing a share level"):]
	if first := strings.Index(guide, alwaysPrivate); first < 0 || first > strings.Index(guide, "| # | Use case") {
		t.Error("the guide must state up front that logins are always private and history only shared in a history group")
	}
	// Spec 003: the README explains history groups and the wrapper variable.
	for _, want := range []string{"## History groups", "aprofile group create", "--group home", "--no-group", "APROFILE_HISTORY_GROUP", "at the same time"} {
		if !strings.Contains(readme, want) {
			t.Errorf("README lacks %q", want)
		}
	}
}
