package gitx

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactorMasksAcrossWrites(t *testing.T) {
	var got bytes.Buffer
	token := "secret-token"
	r := NewRedactor(&got, token)
	for _, chunk := range []string{"prefix secr", "et-token and ", base64.StdEncoding.EncodeToString([]byte(token))[:8], base64.StdEncoding.EncodeToString([]byte(token))[8:], " suffix"} {
		if _, err := r.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String(), token) || strings.Contains(got.String(), base64.StdEncoding.EncodeToString([]byte(token))) {
		t.Fatalf("secret leaked: %q", got.String())
	}
	if got.String() != "prefix *** and *** suffix" {
		t.Fatalf("unexpected redaction %q", got.String())
	}
}

func TestRedactorMasksBasicCredential(t *testing.T) {
	var got bytes.Buffer
	credential := "alice:private-token"
	encoded := base64.StdEncoding.EncodeToString([]byte(credential))
	r := NewRedactor(&got, "private-token", credential)
	for _, chunk := range []string{"Authorization: Basic " + encoded[:5], encoded[5:11], encoded[11:]} {
		if _, err := r.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.String(), encoded) || strings.Contains(got.String(), credential) || strings.Contains(got.String(), "private-token") {
		t.Fatalf("credential leaked: %q", got.String())
	}
	if !strings.Contains(got.String(), "Authorization: Basic ***") {
		t.Fatalf("encoded credential was not masked: %q", got.String())
	}
}

func TestAuthEnvScopesHostAndPort(t *testing.T) {
	env, err := AuthEnv("https://git.example:8443/org/repo.git", "alice", "token", "/data/home")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_KEY_0=http.https://git.example:8443/.extraHeader") {
		t.Fatalf("missing host-scoped key: %s", joined)
	}
	if !strings.Contains(joined, "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("alice:token"))) {
		t.Fatalf("missing authorization value: %s", joined)
	}
	if strings.Contains(joined, "token") {
		t.Fatalf("raw token leaked: %s", joined)
	}
	if _, err := AuthEnv("https://alice:pw@git.example/repo", "alice", "token", "/tmp"); err == nil {
		t.Fatal("accepted URL credentials")
	}
}

func TestRunMasksOutputAndEcho(t *testing.T) {
	var out bytes.Buffer
	token := "secret"
	err := Run(context.Background(), Spec{Args: []string{"sh", "-c", "printf '%s %s' secret " + base64.StdEncoding.EncodeToString([]byte(token))}, Out: &out, Timeout: time.Second, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), token) || strings.Contains(out.String(), base64.StdEncoding.EncodeToString([]byte(token))) {
		t.Fatalf("token leaked: %q", out.String())
	}
	if !strings.HasPrefix(out.String(), "$ sh -c ") {
		t.Fatalf("missing command header: %q", out.String())
	}
}

func TestRunDoesNotPutTokenInArgv(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "argv")
	program := filepath.Join(dir, "capturegit")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_FILE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CAPTURE_FILE", capture)
	token := "token-not-in-argv"
	var out bytes.Buffer
	err := Run(context.Background(), Spec{Args: []string{"capturegit", "fetch", "origin", "main"}, Env: append(os.Environ(), "CAPTURE_FILE="+capture), Out: &out, Timeout: time.Second, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), token) || strings.Contains(out.String(), token) {
		t.Fatalf("token leaked into argv/output: %q / %q", args, out.String())
	}
}

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	var out bytes.Buffer
	started := time.Now()
	marker := filepath.Join(dir, "survived")
	command := `sh -c "trap '' TERM; sleep 30; touch ` + marker + `" & echo $! > ` + pidFile + `; wait`
	err := Run(context.Background(), Spec{ShellCmd: command, Out: &out, Timeout: 100 * time.Millisecond})
	var timedOut *TimeoutError
	if !errors.As(err, &timedOut) {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(started) < 5*time.Second {
		t.Fatal("process group was not given the full TERM grace period")
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		t.Fatal("child PID was not recorded")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child survived process group kill (marker stat error %v)", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected echoed command")
	}
}
