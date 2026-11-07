//go:build aprofile_testseam

package profile

import "os"

// testSeamRunning fixes RunningProcess's answer in binaries built with the
// aprofile_testseam tag, which only the test helpers use (testutil.BuildBinary), so
// integration tests do not depend on whether the sandbox lets them read other
// processes' environments (T092, SC-009). APROFILE_TEST_RUNNING selects the
// answer (see seamAnswer); unset or empty means "none", and "real" runs the real
// process scan. Production builds compile running_testseam_off.go instead and never
// read the variable.
func testSeamRunning() (Process, bool, bool, bool) {
	v := os.Getenv("APROFILE_TEST_RUNNING")
	if v == "" {
		v = "none"
	}
	return seamAnswer(v)
}
