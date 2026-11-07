package testutil

import (
	"os"
	"testing"
)

// TQ12: HermeticHome pins PATH and drops inherited APROFILE_* variables.
func TestHermeticHomePinsPATHAndClearsAprofileVars(t *testing.T) {
	t.Setenv("PATH", "/somewhere/real/bin:/usr/bin:/bin")
	t.Setenv("APROFILE_CONFIG_DIR", "/real/profile")
	t.Setenv("APROFILE_SET_VARS", "ANTHROPIC_API_KEY")
	HermeticHome(t)
	if got := os.Getenv("PATH"); got != HermeticPATH {
		t.Errorf("PATH = %q, want %q", got, HermeticPATH)
	}
	for _, k := range []string{"APROFILE_CONFIG_DIR", "APROFILE_SET_VARS"} {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s still set to %q", k, v)
		}
	}
}
