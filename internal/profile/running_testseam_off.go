//go:build !aprofile_testseam

package profile

// testSeamRunning is the production build of the RunningProcess test seam: it never
// answers, so only the real process scan decides (T092). See
// running_testseam_on.go for the aprofile_testseam test build.
func testSeamRunning() (Process, bool, bool, bool) { return Process{}, false, false, false }
