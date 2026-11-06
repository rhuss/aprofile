package harness

import "errors"

// ParseBuiltinForTest parses data with the built-in validation rules, so tests can
// check what a built-in definition may and may not contain.
func ParseBuiltinForTest(data []byte) (*Definition, error) {
	d, lerr := parseDefinitionOpts(data, "builtin-test.yaml", true)
	if lerr != nil {
		return nil, errors.New(lerr.Error())
	}
	return d, nil
}
