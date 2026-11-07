package profile

import "testing"

// T092: the test seam's answers, parsed the same way in every build.
func TestSeamAnswer(t *testing.T) {
	cases := []struct {
		v                  string
		ok, running, known bool
		pid                int
	}{
		{"none", true, false, true, 0},
		{"unknown", true, false, false, 0},
		{"4242", true, true, true, 4242},
		{"real", false, false, false, 0},
		{"", false, false, false, 0},
		{"-1", false, false, false, 0},
		{"0", false, false, false, 0},
		{"bogus", false, false, false, 0},
	}
	for _, c := range cases {
		proc, running, known, ok := seamAnswer(c.v)
		if ok != c.ok || running != c.running || known != c.known || proc.PID != c.pid {
			t.Errorf("seamAnswer(%q) = (%+v, %v, %v, %v), want pid %d running %v known %v ok %v",
				c.v, proc, running, known, ok, c.pid, c.running, c.known, c.ok)
		}
	}
}
