package harness

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cc-deck/aprofile/internal/secret"
)

// DefaultHookTimeout bounds every hook operation except login and pre-launch
// (FR-011). It is the single source of truth for the timeout.
const DefaultHookTimeout = 10 * time.Second

// defaultHookTimeout is the timeout the Run*Hook functions apply; tests shorten it.
var defaultHookTimeout = DefaultHookTimeout

// hookUnsupportedExit is the exit status a hook returns for an operation it does
// not implement (FR-011); aprofile treats it as a no-op.
const hookUnsupportedExit = 64

// ErrHookUnsupported signals a hook exited 64 (operation not supported). Callers
// treat it as a no-op.
var ErrHookUnsupported = errors.New("exec hook: operation not supported (exit 64)")

// ErrHookNotFound signals the aprofile-harness-<name> executable is not on PATH.
// For the operations that shape the launch (env, and pre-launch in the wrapper) a
// declared hook whose executable is missing is an error (FR-011). For the optional
// operations it is reported instead: auth-status yields AuthStatus.NotInstalled,
// write-config returns this error for sync to report as a note, and login falls
// back to the definition's login_command.
var ErrHookNotFound = errors.New("exec hook: executable not found on PATH")

// Hook is the client for an aprofile-harness-<name> executable (FR-011). It calls
// the executable with the operation name as the sole argument, writes key=value
// lines on stdin and parses key=value lines from stdout. It contains no
// harness-name branching: the harness name only builds the executable name.
type Hook struct {
	// Harness names the harness; the executable is "aprofile-harness-<Harness>".
	Harness string
	// binary, when set, is the explicit executable path (a test seam); otherwise the
	// executable is resolved on PATH.
	binary string
	// timeout bounds the call; 0 means DefaultHookTimeout (a test seam: production
	// calls always use the default, FR-011).
	timeout time.Duration
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

// executable resolves the hook executable path.
func (h *Hook) executable() (string, error) {
	if h.binary != "" {
		return h.binary, nil
	}
	name := HookExecutable(h.Harness)
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrHookNotFound, name)
	}
	return p, nil
}

// effectiveTimeout returns the configured timeout, applying the default.
func (h *Hook) effectiveTimeout() time.Duration {
	if h.timeout <= 0 {
		return defaultHookTimeout
	}
	return h.timeout
}

// Bounds on what aprofile keeps of a hook's output (FR-011). Standard output above
// maxHookStdout is a protocol error; standard error is only quoted in error messages,
// so it is cut at maxHookStderr.
const (
	maxHookStdout = 1 << 20
	maxHookStderr = 4 << 10
)

// cappedBuffer keeps at most max bytes and records whether more arrived. It always
// accepts the whole write, so the hook never sees a broken pipe because of the cap.
type cappedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	room := c.max - c.buf.Len()
	switch {
	case room <= 0:
		c.over = c.over || len(p) > 0
	case len(p) > room:
		c.buf.Write(p[:room])
		c.over = true
	default:
		c.buf.Write(p)
	}
	return len(p), nil
}

// text returns the kept output, trimmed, marked when it was cut.
func (c *cappedBuffer) text() string {
	t := strings.TrimSpace(c.buf.String())
	if c.over {
		t += " ... (truncated)"
	}
	return t
}

// hookTimeouts remembers the hook operations that timed out in this process, so one
// run (a list of many profiles, a show, a doctor, a sync) waits for a hanging hook a
// bounded number of times, not once per call (FR-011). The key is the resolved
// executable and the operation; per key it records which profiles timed out and when.
// A call is answered from the memo, without running the hook, when
//   - the same profile timed out on this operation (the same input would hang again), or
//   - hookTimeoutProfiles different profiles did (the hook hangs regardless of the
//     profile, so the rest of the run does not wait for each one).
//
// One profile's timeout alone never fails the operation for another profile. Entries
// expire after hookTimeoutTTL, so a long-lived process (the TUI) calls the hook again
// later, and are discarded when the executable file changes. Other failures are fast
// and may depend on the profile, so they are not remembered.
var hookTimeouts = struct {
	sync.Mutex
	m map[[2]string]*hookTimeoutMemo
}{m: map[[2]string]*hookTimeoutMemo{}}

const (
	// hookTimeoutTTL is how long a remembered timeout answers calls.
	hookTimeoutTTL = 60 * time.Second
	// hookTimeoutProfiles is the number of profiles whose timeout makes the operation
	// fail at once for every profile.
	hookTimeoutProfiles = 2
)

// hookNow is the memo's clock (a test seam).
var hookNow = time.Now

type hookTimeoutMemo struct {
	modTime  time.Time
	size     int64
	err      error                // the latest timeout error
	profiles map[string]time.Time // profile -> when its call timed out
}

// live drops expired profile entries and reports whether the memo still describes
// the executable fi.
func (m *hookTimeoutMemo) live(fi os.FileInfo, now time.Time) bool {
	if !m.modTime.Equal(fi.ModTime()) || m.size != fi.Size() {
		return false
	}
	for p, at := range m.profiles {
		if now.Sub(at) >= hookTimeoutTTL {
			delete(m.profiles, p)
		}
	}
	return len(m.profiles) > 0
}

func rememberedTimeout(bin, op, profile string) error {
	fi, err := os.Stat(bin)
	if err != nil {
		return nil
	}
	hookTimeouts.Lock()
	defer hookTimeouts.Unlock()
	key := [2]string{bin, op}
	m, ok := hookTimeouts.m[key]
	if !ok {
		return nil
	}
	if !m.live(fi, hookNow()) {
		delete(hookTimeouts.m, key)
		return nil
	}
	if _, same := m.profiles[profile]; same {
		return fmt.Errorf("%w (not called again for this profile for %s)", m.err, hookTimeoutTTL)
	}
	if n := len(m.profiles); n >= hookTimeoutProfiles {
		return fmt.Errorf("%w (timed out for %d profiles; not called again for any profile for %s)", m.err, n, hookTimeoutTTL)
	}
	return nil
}

func rememberTimeout(bin, op, profile string, err error) {
	fi, serr := os.Stat(bin)
	if serr != nil {
		return
	}
	hookTimeouts.Lock()
	defer hookTimeouts.Unlock()
	key := [2]string{bin, op}
	now := hookNow()
	m, ok := hookTimeouts.m[key]
	if !ok || !m.live(fi, now) {
		m = &hookTimeoutMemo{modTime: fi.ModTime(), size: fi.Size(), profiles: map[string]time.Time{}}
		hookTimeouts.m[key] = m
	}
	m.err = err
	m.profiles[profile] = now
}

// Call runs one operation per the FR-011 protocol. in supplies the stdin
// key=value lines (it must already carry harness/profile/config_dir and any
// operation-specific keys). It returns ErrHookUnsupported on exit 64, ErrHookNotFound
// when the executable is missing, and an error naming the hook and operation on a
// timeout, any other non-zero exit, output that is not key=value lines, or standard
// output above 1 MiB. On a timeout the hook's whole process group is killed, and the
// operation is not called again for the same profile (in["profile"]) for a while, nor
// for any profile once it timed out for two (see hookTimeouts): those calls return
// the timeout at once.
func (h *Hook) Call(op string, in map[string]string) (HookResult, error) {
	bin, err := h.executable()
	if err != nil {
		return HookResult{}, err
	}
	profile := in["profile"]
	if terr := rememberedTimeout(bin, op, profile); terr != nil {
		return HookResult{}, terr
	}

	stdin, err := encodeHookInput(in)
	if err != nil {
		return HookResult{}, fmt.Errorf("exec hook %q %s: %w", h.Harness, op, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.effectiveTimeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, op)
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := &cappedBuffer{max: maxHookStdout}
	stderr := &cappedBuffer{max: maxHookStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	// The hook runs in its own process group, and a timeout kills the group, so no
	// child of a shell hook survives it (FR-011). WaitDelay is the backstop where
	// process groups are not available: Wait closes the pipes and returns shortly
	// after the deadline even if a child still holds them.
	killProcessGroupOnCancel(cmd)
	cmd.WaitDelay = time.Second

	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		terr := fmt.Errorf("exec hook %q %s: timed out after %s", h.Harness, op, h.effectiveTimeout())
		rememberTimeout(bin, op, profile, terr)
		return HookResult{}, terr
	}
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			if ee.ExitCode() == hookUnsupportedExit {
				return HookResult{}, ErrHookUnsupported
			}
			return HookResult{}, fmt.Errorf("exec hook %q %s: exit %d: %s", h.Harness, op, ee.ExitCode(), stderr.text())
		}
		return HookResult{}, fmt.Errorf("exec hook %q %s: %w", h.Harness, op, runErr)
	}
	if stdout.over {
		return HookResult{}, fmt.Errorf("exec hook %q %s: output exceeds %d bytes", h.Harness, op, maxHookStdout)
	}

	res, perr := parseHookOutput(stdout.buf.Bytes())
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
	// NotInstalled is set when the definition declares auth-status but the hook
	// executable is not on PATH; Status is then empty.
	NotInstalled bool
}

// RunAuthStatusHook calls the auth-status operation when the definition declares it,
// so show and doctor can report whether a profile is logged in (FR-011, FR-044). Exit
// 64 is a no-op (empty status, nil error); a missing executable is not an error either
// but sets NotInstalled, so show can say why there is no status. Any other failure or
// non-key=value output is returned as an error naming the hook and operation.
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
	if errors.Is(err, ErrHookNotFound) {
		return AuthStatus{NotInstalled: true}, nil
	}
	if errors.Is(err, ErrHookUnsupported) {
		return AuthStatus{}, nil
	}
	if err != nil {
		return AuthStatus{}, err
	}
	status, _ := res.Get("status")
	detail, _ := res.Get("detail")
	return AuthStatus{Status: status, Detail: detail}, nil
}

// RunLoginHook calls the login operation (FR-011, FR-039). Standard input carries
// the key=value lines (harness, profile, config_dir, auth_kind); standard output and
// error go to stdout and stderr unparsed, because the login is interactive (a hook
// that needs keyboard input reads /dev/tty). There is no timeout. env is the
// subprocess environment (the profile's login environment).
//
// It returns ErrHookUnsupported when the definition does not declare the operation
// or the hook exits 64, ErrHookNotFound when the executable is missing, and an error
// naming the hook and operation for any other non-zero exit.
func RunLoginHook(def *Definition, ctx HookContext, env []string, stdout, stderr io.Writer) error {
	if !def.HookDeclares("login") {
		return ErrHookUnsupported
	}
	in := ctx.baseInput()
	if ctx.AuthKind != "" {
		in["auth_kind"] = ctx.AuthKind
	}
	h := &Hook{Harness: def.Name}
	bin, err := h.executable()
	if err != nil {
		return err
	}
	stdin, err := encodeHookInput(in)
	if err != nil {
		return fmt.Errorf("exec hook %q login: %w", h.Harness, err)
	}
	cmd := exec.Command(bin, "login")
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if env != nil {
		cmd.Env = env
	}
	if runErr := cmd.Run(); runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			if ee.ExitCode() == hookUnsupportedExit {
				return ErrHookUnsupported
			}
			return fmt.Errorf("exec hook %q login: exit %d", h.Harness, ee.ExitCode())
		}
		return fmt.Errorf("exec hook %q login: %w", h.Harness, runErr)
	}
	return nil
}

// reservedWrapperVar reports whether name is a variable the wrapper owns and a hook
// must not override: an APROFILE_* contract variable, a config_env redirect, any auth
// variable of the harness, or a variable its auth_clears lists.
func reservedWrapperVar(def *Definition, name string) bool {
	if strings.HasPrefix(name, "APROFILE_") {
		return true
	}
	for _, e := range def.ConfigEnv {
		if e.Var == name {
			return true
		}
	}
	for _, v := range def.UnsetAuthVars() {
		if v == name {
			return true
		}
	}
	return false
}

// RunEnvHook calls the env operation when the definition declares it, returning the
// variables to weave into the wrapper (FR-011). Exit 64 is a no-op (nil, nil). A
// declared env operation whose executable is missing is an error wrapping
// ErrHookNotFound: rendering the wrapper without the hook's variables would launch
// the harness with an incomplete environment (T076). Returned values are subject to FR-028's prefix check only (not
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
	if errors.Is(err, ErrHookNotFound) {
		return nil, missingHookError(def, "env", err)
	}
	if errors.Is(err, ErrHookUnsupported) {
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

// missingHookError wraps ErrHookNotFound with the hook, the operation and the fix.
func missingHookError(def *Definition, op string, err error) error {
	return fmt.Errorf("exec hook %q %s: %w (the definition declares the %s operation; install %s or remove %s from exec_hook.operations)",
		def.Name, op, err, op, HookExecutable(def.Name), op)
}

// HookExecutable is the executable name of a harness's exec hook (FR-011).
func HookExecutable(harnessName string) string { return "aprofile-harness-" + harnessName }

// RunWriteConfigHook calls the write-config operation when the definition declares
// it, after sync prepared the profile config dir (FR-011). It returns the hook's
// changed= answer, so sync reports a change only when the hook made one. Exit 64 or
// an undeclared operation is a no-op (false, nil). A missing executable returns an
// error wrapping ErrHookNotFound, which sync reports without failing the profile.
// Output without a changed=true|false line is a protocol error naming the hook and
// operation, like any other non-zero exit or non key=value output.
func RunWriteConfigHook(def *Definition, ctx HookContext) (bool, error) {
	if !def.HookDeclares("write-config") {
		return false, nil
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
	res, err := h.Call("write-config", in)
	if errors.Is(err, ErrHookNotFound) {
		return false, missingHookError(def, "write-config", err)
	}
	if errors.Is(err, ErrHookUnsupported) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	v, ok := res.Get("changed")
	switch {
	case !ok:
		return false, fmt.Errorf("exec hook %q write-config: output has no changed=true|false line", def.Name)
	case v == "true":
		return true, nil
	case v == "false":
		return false, nil
	default:
		return false, fmt.Errorf("exec hook %q write-config: changed=%q, want true or false", def.Name, redactLine(v))
	}
}
