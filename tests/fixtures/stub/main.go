// Command stub is a no-op harness binary used by wrapper and overhead tests. It
// prints its arguments and environment, then exits 0, so a test can assert that a
// generated wrapper exec'd it with the expected environment (SC-011, US1 launch
// test). It never touches the real home directory.
package main

import (
	"fmt"
	"os"
	"sort"
)

func main() {
	fmt.Printf("STUB_ARGS: %v\n", os.Args[1:])
	env := os.Environ()
	sort.Strings(env)
	for _, kv := range env {
		fmt.Printf("STUB_ENV: %s\n", kv)
	}
}
