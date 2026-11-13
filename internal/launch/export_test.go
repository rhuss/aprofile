package launch

// SetBeforeWrite installs a hook Apply calls after its first plan and before it
// re-reads the files and writes, so a test can simulate the harness writing the same
// file in between. It returns a function restoring the previous hook.
func SetBeforeWrite(f func()) (restore func()) {
	prev := beforeWrite
	beforeWrite = f
	return func() { beforeWrite = prev }
}
