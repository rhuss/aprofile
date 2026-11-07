package profile

import "strconv"

// seamAnswer parses a test seam answer for RunningProcess (T092): "none" (no
// process uses the profile), "unknown" (the process list cannot be read) or a
// positive pid (that process uses it). Any other value, "real" by convention, is
// ok=false: the real process scan answers. Only binaries built with the
// aprofile_testseam tag consult the seam (running_testseam_on.go); in every other
// build this function is unreachable.
func seamAnswer(v string) (proc Process, running, known, ok bool) {
	switch v {
	case "none":
		return Process{}, false, true, true
	case "unknown":
		return Process{}, false, false, true
	}
	if pid, err := strconv.Atoi(v); err == nil && pid > 0 {
		return Process{PID: pid, Name: "aprofile-test-seam"}, true, true, true
	}
	return Process{}, false, false, false
}
