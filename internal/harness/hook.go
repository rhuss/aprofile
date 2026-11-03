package harness

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/cc-deck/aprofile/internal/secret"
)

// DefaultHookTimeout bounds every hook operation except login and pre-launch
// (FR-011). It is the single source of truth for the timeout.
const DefaultHookTimeout = 10 * time.Second

// hookUnsupportedExit is the exit status a hook returns for an operation it does
// not implement (FR-011); aprofile treats it as a no-op.
const hookUnsupportedExit = 64

// ErrHookUnsupported signals a hook exited 64 (operation not supported). Callers
// treat it as a no-op.
var ErrHookUnsupported = errors.New("exec hook: operation not supported (exit 64)")

// ErrHookNotFound signals the aprofile-harness-<name> executable is not on PATH.
// A declared hook whose executable is missing is treated as a no-op at render time
// so wrappers stay renderable without the hook installed.
var ErrHookNotFound = errors.New("exec hook: executable not found on PATH")

// Hook is the client for an aprofile-harness-<name> executable (FR-011). It calls
// the executable with the operation name as the sole argument, writes key=value
// lines on stdin and parses key=value lines from stdout. It contains no
// harness-name branching: the harness name only builds the executable name.
type Hook struct {
	// Harness names the harness; the executable is "aprofile-harness-<Harness>".
	Harness string
	// Binary, when set, is the explicit executable path (used by tests); otherwise
	// the executable is resolved on PATH.
	Binary string
	// Timeout bounds the call; 0 means DefaultHookTimeout.
	Timeout time.Duration
	// NoTimeout disables the timeout entirely (login, pre-launch).
	NoTimeout bool
	// Env is the subprocess environment; nil inherits os.Environ().
	Env []string
}

// HookResult holds a hook's parsed key=value output in order.
type HookResult struct {
	pairs [][2]string
}

// Pairs returns the output key/value pairs in output order.
func (r HookResult) Pairs() [][2]string { return r.pairs }

// Get returns the value for a key and whether it was present.
func (r HookResult) Get(key string) (string, bool) {
	for _, kv := range r.pairs {
		if kv[0] == key {
			return kv[1], true
		}
	}
	return "", false
}

// Map returns the output as a map (last value wins on duplicate keys).
func (r HookResult) Map() map[string]string {
	m := make(map[string]string, len(r.pairs))
	for _, kv := range r.pairs {
		m[kv[0]] = kv[1]
	}
	return m
}

// executable resolves the hook executable path.
func (h *Hook) executable() (string, error) {
	if h.Binary != "" {
		return h.Binary, nil
	}
	name := "aprofile-harness-" + h.Harness
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrHookNotFound, name)
	}
	return p, nil
}

// effectiveTimeout returns the configured timeout, applying the default.
func (h *Hook) effectiveTimeout() time.Duration {
	if h.Timeout <= 0 {
		return DefaultHookTimeout
	}
	return h.Timeout
}

// Call runs one operation per the FR-011 protocol. in supplies the stdin
// key=value lines (it must already carry harness/profile/config_dir and any
// operation-specific keys). It returns ErrHookUnsupported on exit 64, ErrHookNotFound
// when the executable is missing, and an error naming the hook and operation on a
// timeout, any other non-zero exit, or output that is not key=value lines.
func (h *Hook) Call(op string, in map[string]string) (HookResult, error) {
	bin, err := h.executable()
	if err != nil {
		return HookResult{}, err
	}

	stdin, err := encodeHookInput(in)
	if err != nil {
		return HookResult{}, fmt.Errorf("exec hook %q %s: %w", h.Harness, op, err)
	}

	ctx := context.Background()
	if !h.NoTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.effectiveTimeout())
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, bin, op)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if h.Env != nil {
		cmd.Env = h.Env
	} else {
		cmd.Env = os.Environ()
	}
	// exec.CommandContext kills only the direct child on timeout. A shell hook whose
	// grandchild (for example `sleep`) still holds the stdout pipe would otherwise keep
	// Wait blocked for the grandchild's lifetime, defeating the timeout (FR-011).
	// WaitDelay forces Wait to close the pipes and return shortly after the deadline.
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return HookResult{}, fmt.Errorf("exec hook %q %s: timed out after %s", h.Harness, op, h.effectiveTimeout())
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			if ee.ExitCode() == hookUnsupportedExit {
				return HookResult{}, ErrHookUnsupported
			}
			return HookResult{}, fmt.Errorf("exec hook %q %s: exit %d: %s", h.Harness, op, ee.ExitCode(), strings.TrimSpace(stderr.String()))
		}
		return HookResult{}, fmt.Errorf("exec hook %q %s: %w", h.Harness, op, runErr)
	}

	res, perr := parseHookOutput(stdout.Bytes())
	if perr != nil {
		return HookResult{}, fmt.Errorf("exec hook %q %s: %w", h.Harness, op, perr)
	}
	return res, nil
}

// encodeHookInput renders the stdin key=value lines, keys sorted for determinism.
func encodeHookInput(in map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	for _, k := range keys {
		v := in[k]
		if k == "" || strings.ContainsAny(k, "=\n\r") || strings.ContainsAny(v, "\n\r") {
			return nil, fmt.Errorf("invalid input key/value for %q", k)
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}

// parseHookOutput parses key=value lines; a non-empty line without '=' aborts.
func parseHookOutput(data []byte) (HookResult, error) {
	var res HookResult
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			return HookResult{}, fmt.Errorf("non key=value output line: %q", redactLine(line))
		}
		res.pairs = append(res.pairs, [2]string{line[:i], line[i+1:]})
	}
	if err := sc.Err(); err != nil {
		return HookResult{}, fmt.Errorf("read hook output: %w", err)
	}
	return res, nil
}

func redactLine(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// EnvVar is one environment variable a hook's env operation returns.
type EnvVar struct {
	Name  string
	Value string
}

// HookContext carries the values every hook call receives plus the optional
// operation inputs (FR-011).
type HookContext struct {
	Harness   string
	Profile   string
	ConfigDir string
	AuthKind  string
	Model     string
	Endpoint  string
}

// baseInput builds the always-present stdin keys.
func (c HookContext) baseInput() map[string]string {
	return map[string]string{
		"harness":    c.Harness,
		"profile":    c.Profile,
		"config_dir": c.ConfigDir,
	}
}

// HookDeclares reports whether the definition's exec hook lists the operation
// (FR-011: aprofile calls only the operations the definition lists).
func (d *Definition) HookDeclares(op string) bool {
	if d.ExecHook == nil {
		return false
	}
	for _, o := range d.ExecHook.Operations {
		if o == op {
			return true
		}
	}
	return false
}

// AuthStatus is the parsed result of a hook's auth-status operation (FR-011): a
// status of ok|missing|unknown and an optional human-readable detail.
type AuthStatus struct {
	Status string
	Detail string
}

// RunAuthStatusHook calls the auth-status operation when the definition declares it,
// so show and doctor can report whether a profile is logged in (FR-011, FR-044). A
// missing executable or exit 64 is a no-op (empty status, nil error). Any other failure
// or non-key=value output is returned as an error naming the hook and operation.
func RunAuthStatusHook(def *Definition, ctx HookContext) (AuthStatus, error) {
	if !def.HookDeclares("auth-status") {
		return AuthStatus{}, nil
	}
	in := ctx.baseInput()
	if ctx.AuthKind != "" {
		in["auth_kind"] = ctx.AuthKind
	}
	h := &Hook{Harness: def.Name}
	res, err := h.Call("auth-status", in)
	if errors.Is(err, ErrHookUnsupported) || errors.Is(err, ErrHookNotFound) {
		return AuthStatus{}, nil
	}
	if err != nil {
		return AuthStatus{}, err
	}
	status, _ := res.Get("status")
	detail, _ := res.Get("detail")
	return AuthStatus{Status: status, Detail: detail}, nil
}

// reservedWrapperVar reports whether name is a variable the wrapper owns and a hook
// must not override: an APROFILE_* contract variable, a config_env redirect, or any auth
// variable of the harness.
func reservedWrapperVar(def *Definition, name string) bool {
	if strings.HasPrefix(name, "APROFILE_") {
		return true
	}
	for _, e := range def.ConfigEnv {
		if e.Var == name {
			return true
		}
	}
	for _, v := range def.AllAuthVars() {
		if v == name {
			return true
		}
	}
	return false
}

// RunEnvHook calls the env operation when the definition declares it, returning the
// variables to weave into the wrapper (FR-011). A missing executable or exit 64 is a
// no-op (nil, nil). Returned values are subject to FR-028's prefix check only (not
// the length check), so a hook may emit long paths and URLs but never a known secret
// prefix; a reference (env:/file:) passes through to be resolved at launch.
func RunEnvHook(def *Definition, ctx HookContext) ([]EnvVar, error) {
	if !def.HookDeclares("env") {
		return nil, nil
	}
	in := ctx.baseInput()
	if ctx.AuthKind != "" {
		in["auth_kind"] = ctx.AuthKind
	}
	if ctx.Model != "" {
		in["model"] = ctx.Model
	}
	if ctx.Endpoint != "" {
		in["endpoint"] = ctx.Endpoint
	}

	h := &Hook{Harness: def.Name}
	res, err := h.Call("env", in)
	if errors.Is(err, ErrHookUnsupported) || errors.Is(err, ErrHookNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []EnvVar
	for _, kv := range res.Pairs() {
		name, val := kv[0], kv[1]
		if !secret.ValidEnvVarName(name) {
			return nil, fmt.Errorf("exec hook %q env: variable name %q must match [A-Za-z_][A-Za-z0-9_]*", def.Name, name)
		}
		// A hook must not set an auth variable, a config_env variable, or an APROFILE_*
		// contract variable: the wrapper owns those (it clears auth vars, so a hook-set
		// auth var would defeat the isolation the wrapper just enforced, FR-027).
		if reservedWrapperVar(def, name) {
			return nil, fmt.Errorf("exec hook %q env: variable %q is reserved (auth, config_env or APROFILE_* contract) and cannot be set by the hook", def.Name, name)
		}
		if secret.IsReference(val) {
			if _, err := secret.ParseReference(val); err != nil {
				return nil, fmt.Errorf("exec hook %q env: variable %q: %w", def.Name, name, err)
			}
		} else if err := secret.CheckPrefix(val); err != nil {
			return nil, fmt.Errorf("exec hook %q env: variable %q: %w", def.Name, name, err)
		}
		out = append(out, EnvVar{Name: name, Value: val})
	}
	return out, nil
}
