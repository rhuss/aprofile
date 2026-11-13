package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// FR-035/SC-005: an args element that reads as a path survives conversion even when
// it is long, also as the value of a `--flag=value` element and as a bare file name
// with an extension; a secret value, a prefix token disguised with an extension and
// a long opaque token are still dropped.
func TestPathArgsAreNotOverDropped(t *testing.T) {
	keep := []string{
		"--dir=/Users/x/very/long/path/to/some/project/dir",
		"-d=/Users/x/very/long/path/to/some/project/dir",
		"--config=./relative/long/path/to/the/config/file.json",
		"my-really-long-server-file-name-x.js",
		"--entry=my-really-long-server-file-name-x.js",
		"very-long-python-module-entry-point.py",
	}
	for _, a := range keep {
		if isSecretArg(a) {
			t.Errorf("isSecretArg(%q) = true, want false: the value reads as a path", a)
		}
	}
	drop := []string{
		"--token=" + argBlob,                                          // length check on the value
		"--token=" + argEqToken,                                       // prefix check on the value
		"-t=" + argEqToken,                                            // short flag
		"ghp_CANARYARGTOKEN0000000000000000000000abc.js",              // prefix token with a fake extension; betterleaks:allow
		"--file=ghp_CANARYARGTOKEN0000000000000000000000abc.js",       // betterleaks:allow
		"--dir=/Users/x/ghp_CANARYARGTOKEN0000000000000000000000/dir", // prefix check on each path segment; betterleaks:allow
		argBlob,                                // long opaque token, no extension
		"AbCdEf0123456789AbCdEf0123456789XyZ0", // mixed letters/digits, no extension
	}
	for _, a := range drop {
		if !isSecretArg(a) {
			t.Errorf("isSecretArg(%q) = false, want true", a)
		}
	}
}

// The path args survive a real conversion into every flavor.
func TestPathArgsSurviveConversion(t *testing.T) {
	args := []string{"--dir=/Users/x/very/long/path/to/some/project/dir", "my-really-long-server-file-name-x.js"}
	for _, flavor := range allFlavors {
		s := Server{Name: "s", Transport: TransportStdio, Command: "node", Args: args}
		data, rep, err := MergeInto(flavor, nil, []Server{s}, false)
		if err != nil {
			t.Fatalf("%s: %v", flavor, err)
		}
		if len(rep.Drops) != 0 || len(rep.Skipped) != 0 {
			t.Errorf("%s: path args dropped: %+v", flavor, rep)
		}
		got, _ := ParseDoc(flavor, data)
		if len(got) != 1 || strings.Join(got[0].Args, " ") != strings.Join(args, " ") {
			t.Errorf("%s: args not preserved: %+v", flavor, got)
		}
	}
}

// A report with nothing converted marshals converted as [], never null.
func TestConversionReportConvertedNeverNull(t *testing.T) {
	for _, r := range []ConversionReport{{}, SkipAll([]Server{{Name: "s"}}, "no mcp")} {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"converted":[]`) {
			t.Errorf("converted should be [], got %s", b)
		}
	}
}
