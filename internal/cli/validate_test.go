package cli

import (
	"strings"
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
	"github.com/cc-deck/aprofile/internal/testutil"
)

// TestModelEndpointRejectedForUnsupportedHarness proves FR-003's limitation is enforced
// rather than silently dropped: --model / --endpoint error for a harness whose
// definition declares no backing variable (OpenCode declares neither in slice one).
func TestModelEndpointRejectedForUnsupportedHarness(t *testing.T) {
	set, err := harness.LoadBuiltins()
	if err != nil {
		t.Fatalf("LoadBuiltins: %v", err)
	}
	def, ok := set.Get("opencode")
	if !ok {
		t.Skip("opencode definition not present")
	}
	if def.ModelVar != "" || def.EndpointVar != "" {
		t.Skipf("opencode now declares model/endpoint vars; update this test")
	}

	if err := applyValueFlags(&profile.Profile{Harness: "opencode", Name: "x"}, def, &profileFlags{model: "some-model"}); err == nil {
		t.Error("expected --model to be rejected for a harness with no model variable")
	}
	if err := applyValueFlags(&profile.Profile{Harness: "opencode", Name: "x"}, def, &profileFlags{endpoint: "https://x"}); err == nil {
		t.Error("expected --endpoint to be rejected for a harness with no endpoint variable")
	}
	// A claude profile with a model is accepted (it declares the var).
	cl, ok := set.Get("claude")
	if ok && cl.ModelVar != "" {
		if err := applyValueFlags(&profile.Profile{Harness: "claude", Name: "x"}, cl, &profileFlags{model: "sonnet"}); err != nil {
			t.Errorf("claude --model should be accepted: %v", err)
		}
	}
}

// Feature 004 FR-023: Codex accepts --model now that its definition passes the model
// as a launch argument; add, edit and show agree, and the model is not reported as
// ignored.
func TestCodexAcceptsModel(t *testing.T) {
	set, _ := harness.LoadBuiltins()
	cx, _ := set.Get("codex")
	if err := applyValueFlags(&profile.Profile{Harness: "codex", Name: "x"}, cx, &profileFlags{model: "1.5"}); err != nil {
		t.Errorf("codex --model should be accepted: %v", err)
	}
	testutil.HermeticHome(t)
	if out, err := runCLI(t, "add", "codex", "glm", "--auth-kind", "chatgpt-login", "--model", "glm-5"); err != nil {
		t.Fatalf("add codex --model: %v\n%s", err, out)
	}
	if out, err := runCLI(t, "edit", "codex/glm", "--model", "glm-6"); err != nil {
		t.Fatalf("edit codex --model: %v\n%s", err, out)
	}
	out, _ := runCLI(t, "show", "codex/glm")
	if !strings.Contains(out, "glm-6") || strings.Contains(out, "ignored") {
		t.Errorf("show codex/glm:\n%s", out)
	}
	// A model with a control character cannot be a TOML basic string: rejected.
	if out, err := runCLI(t, "edit", "codex/glm", "--model", "bad\x01model"); err == nil {
		t.Errorf("a model with a control character must be rejected:\n%s", out)
	}
}
