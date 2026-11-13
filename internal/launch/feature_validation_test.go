package launch_test

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/launch"
)

func TestPlanRejectsHarnessInvalidStoredFeatureBeforeRemovingEntries(t *testing.T) {
	d := builtin(t, "codex")
	p := codexFeatureProfile(t, map[string]string{"traces": "on", "endpoint": "https://e"})
	good := plan(t, p, d, codexDoc(t, ""), launch.Record{})
	p.Features[0].Params["protocol"] = "http/protobuf"
	res, err := launch.Plan(p, d, codexDoc(t, string(good.Writes()["config.toml"])), good.NewRecord)
	if err == nil {
		t.Fatalf("invalid stored protocol silently planned changes: %+v", res.Steps)
	}
	for _, want := range []string{"http/protobuf", "codex", "aprofile features codex/work-glm"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if len(res.Writes()) != 0 {
		t.Errorf("invalid state planned writes: %v", res.Writes())
	}
}
