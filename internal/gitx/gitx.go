// Package gitx runs Git and shell commands with bounded execution and secret-safe output.
package gitx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Spec describes one child process. Args and ShellCmd are mutually exclusive.
type Spec struct {
	Dir      string
	Args     []string
	ShellCmd string
	Env      []string
	Out      io.Writer
	Timeout  time.Duration
	Token    string
	Redact   []string
}

var ErrTimedOut = errors.New("command timed out")

// TimeoutError distinguishes a deadline from an ordinary child exit.
type TimeoutError struct{ Timeout time.Duration }

func (e *TimeoutError) Error() string { return fmt.Sprintf("command timed out after %s", e.Timeout) }
func (e *TimeoutError) Unwrap() error { return ErrTimedOut }

// Run executes a command in its own process group, streams combined output, and masks credentials.
func Run(ctx context.Context, spec Spec) error {
	if (len(spec.Args) == 0) == (spec.ShellCmd == "") {
		return errors.New("gitx: exactly one of Args or ShellCmd is required")
	}
	if ctx == nil {
		return errors.New("gitx: context is nil")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("command canceled: %w", err)
	}
	if spec.Out == nil {
		spec.Out = io.Discard
	}
	var name string
	var args []string
	if spec.ShellCmd != "" {
		name, args = "sh", []string{"-c", spec.ShellCmd}
	} else {
		name, args = spec.Args[0], spec.Args[1:]
	}
	if spec.Timeout <= 0 {
		return errors.New("gitx: timeout must be greater than zero")
	}
	if _, err := fmt.Fprintf(spec.Out, "$ %s\n", display(name, args, spec.Token, spec.Redact...)); err != nil {
		return fmt.Errorf("gitx: write command header: %w", err)
	}
	redactor := NewRedactor(spec.Out, spec.Token, spec.Redact...)
	cmd := exec.Command(name, args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdout, cmd.Stderr = redactor, redactor
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = redactor.Close()
		return fmt.Errorf("start command: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	timer := time.NewTimer(spec.Timeout)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		waitErr = terminate(cmd.Process.Pid, wait)
		_ = redactor.Close()
		return fmt.Errorf("command canceled: %w", ctx.Err())
	case <-timer.C:
		_ = terminate(cmd.Process.Pid, wait)
		_ = redactor.Close()
		return &TimeoutError{Timeout: spec.Timeout}
	}
	if err := redactor.Close(); err != nil {
		return fmt.Errorf("flush command output: %w", err)
	}
	if waitErr != nil {
		return fmt.Errorf("command: %w", waitErr)
	}
	return nil
}

func terminate(pid int, wait <-chan error) error {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-wait:
		// Keep the grace period for descendants that outlive the direct child.
		<-timer.C
	case <-timer.C:
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		waitErr = <-wait
		return waitErr
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	return waitErr
}

func display(name string, args []string, token string, extra ...string) string {
	parts := append([]string{name}, args...)
	for i, part := range parts {
		secrets := append([]string{token}, extra...)
		for _, secret := range secrets {
			if secret != "" {
				part = strings.ReplaceAll(part, secret, "***")
				part = strings.ReplaceAll(part, base64.StdEncoding.EncodeToString([]byte(secret)), "***")
			}
		}
		if strings.ContainsAny(part, " \t\n\"'") {
			part = "\"" + strings.ReplaceAll(strings.ReplaceAll(part, "\\", "\\\\"), "\"", "\\\"") + "\""
		}
		parts[i] = part
	}
	return strings.Join(parts, " ")
}

// Redactor masks token and base64(token) in a stream, including matches crossing writes.
type Redactor struct {
	mu       sync.Mutex
	out      io.Writer
	patterns [][]byte
	pending  []byte
	closed   bool
}

func NewRedactor(out io.Writer, token string, additional ...string) *Redactor {
	r := &Redactor{out: out}
	secrets := append([]string{token}, additional...)
	for _, secret := range secrets {
		if secret != "" {
			r.patterns = append(r.patterns, []byte(secret))
			encoded := base64.StdEncoding.EncodeToString([]byte(secret))
			if encoded != secret {
				r.patterns = append(r.patterns, []byte(encoded))
			}
		}
	}
	return r
}

func (r *Redactor) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, errors.New("redactor is closed")
	}
	n := len(p)
	data := append(r.pending, p...)
	keep := r.maxPatternLen() - 1
	cut := len(data) - keep
	if cut < 0 {
		cut = 0
	}
	consumed, err := r.writeMasked(data, cut)
	if err != nil {
		return 0, err
	}
	r.pending = append(r.pending[:0], data[consumed:]...)
	return n, nil
}

func (r *Redactor) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	_, err := r.writeMasked(r.pending, len(r.pending))
	return err
}

func (r *Redactor) maxPatternLen() int {
	max := 1
	for _, p := range r.patterns {
		if len(p) > max {
			max = len(p)
		}
	}
	return max
}

func (r *Redactor) writeMasked(src []byte, limit int) (int, error) {
	if len(src) == 0 || limit <= 0 {
		return 0, nil
	}
	var out bytes.Buffer
	i := 0
	for i < limit {
		matched := false
		for _, pattern := range r.patterns {
			if len(pattern) > 0 && bytes.HasPrefix(src[i:], pattern) {
				out.WriteString("***")
				i += len(pattern)
				matched = true
				break
			}
		}
		if !matched {
			out.WriteByte(src[i])
			i++
		}
	}
	_, err := r.out.Write(out.Bytes())
	return i, err
}

// AuthEnv returns Git's config-based HTTP Basic authentication environment. It leaves the URL out of argv.
func AuthEnv(repoURL, username, token, home string) ([]string, error) {
	base := []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
	if token == "" {
		return base, nil
	}
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid repository URL for authentication")
	}
	if parsed.User != nil {
		return nil, errors.New("repository URL must not contain embedded credentials")
	}
	value := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	return append(base,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http."+parsed.Scheme+"://"+parsed.Host+"/.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic "+value,
	), nil
}

// MakeHome creates Git's isolated home directory.
func MakeHome(dataDir string) (string, error) {
	home := filepath.Join(dataDir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("create git home: %w", err)
	}
	if err := os.Chmod(home, 0o700); err != nil {
		return "", fmt.Errorf("set git home permissions: %w", err)
	}
	return home, nil
}
