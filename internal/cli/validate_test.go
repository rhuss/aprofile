package cli

import (
	"testing"

	"github.com/cc-deck/aprofile/internal/harness"
	"github.com/cc-deck/aprofile/internal/profile"
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
