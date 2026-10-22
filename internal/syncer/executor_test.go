package syncer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitsync/internal/gitx"
	"gitsync/internal/model"
)

func TestExecutorClonePullAndDefaultBranch(t *testing.T) {
	remote, seed, bare := makeRemote(t)
	e := newTestExecutor(t, map[string]string{remote: bare})
	repo := model.Repository{Name: "sample", URL: remote, Path: filepath.Join(t.TempDir(), "clone"), AccessUsername: "git"}
	var log bytes.Buffer
	result, err := e.Run(context.Background(), repo, Request{Trigger: model.TriggerManual, Source: model.SourceManual}, &log)
	if err != nil {
		t.Fatalf("clone: %v\n%s", err, log.String())
	}
	if result.Action != model.ActionClone || result.CommitBefore != "" || !result.Changed || result.ResolvedBranch != "main" {
		t.Fatalf("unexpected clone result: %+v", result)
	}
	if !strings.Contains(log.String(), "[gitsync] --- clone ---") || !strings.Contains(log.String(), "[gitsync] result: success") {
		t.Fatalf("missing sections: %s", log.String())
	}
	result, err = e.Run(context.Background(), repo, Request{}, &log)
	if err != nil {
		t.Fatalf("unchanged pull: %v", err)
	}
	if result.Action != model.ActionPull || result.Changed || !strings.HasPrefix(result.Summary, "Already up to date") {
		t.Fatalf("unexpected no-op result: %+v", result)
	}
	commit(t, seed, "updated")
	result, err = e.Run(context.Background(), repo, Request{}, &log)
	if err != nil {
		t.Fatalf("updated pull: %v\n%s", err, log.String())
	}
	if !result.Changed || !strings.HasPrefix(result.Summary, "Updated ") {
		t.Fatalf("unexpected update result: %+v", result)
	}
}

func TestExecutorExplicitBranchClone(t *testing.T) {
	remote, seed, bare := makeRemote(t)
	run(t, seed, "git", "checkout", "-b", "stable")
	commit(t, seed, "stable commit")
	run(t, seed, "git", "push", "-u", "origin", "stable")
	e := newTestExecutor(t, map[string]string{remote: bare})
	repo := model.Repository{Name: "sample", URL: remote, Branch: "stable", Path: filepath.Join(t.TempDir(), "clone"), AccessUsername: "git"}
	result, err := e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedBranch != "stable" {
		t.Fatalf("branch = %q", result.ResolvedBranch)
	}
}

func TestExecutorRejectsNonRepoAndOriginMismatch(t *testing.T) {
	remote, _, bare := makeRemote(t)
	other, _, otherBare := makeRemote(t)
	e := newTestExecutor(t, map[string]string{remote: bare, other: otherBare})
	nonRepo := filepath.Join(t.TempDir(), "not-repo")
	if err := os.Mkdir(nonRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonRepo, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := model.Repository{URL: remote, Path: nonRepo}
	result, err := e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || result.FailedStage != model.StageClone || !strings.Contains(err.Error(), "not a Git repository") {
		t.Fatalf("non-repo result=%+v err=%v", result, err)
	}
	path := filepath.Join(t.TempDir(), "clone")
	repo.Path, repo.URL = path, remote
	if _, err := e.Run(context.Background(), repo, Request{}, ioDiscard{}); err != nil {
		t.Fatalf("prepare mismatch repo: %v", err)
	}
	marker := filepath.Join(t.TempDir(), "post-ran")
	repo.URL, repo.PostCommand = other, "touch "+marker
	result, err = e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "origin URL differs") {
		t.Fatalf("mismatch result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("post command ran after git-stage failure (stat error %v)", err)
	}
}

func TestExecutorPostCommandEnvironmentAndFailure(t *testing.T) {
	remote, _, bare := makeRemote(t)
	e := newTestExecutor(t, map[string]string{remote: bare})
	e.EnvPassthrough = []string{"PASSTHROUGH", "GITSYNC_OIDC_CLIENT_SECRET"}
	t.Setenv("PASSTHROUGH", "copied")
	t.Setenv("GITSYNC_OIDC_CLIENT_SECRET", "must-not-pass")
	repo := model.Repository{Name: "sample", URL: remote, Path: filepath.Join(t.TempDir(), "clone"), PostCommand: "printf '%s|%s|%s|%s|%s|%s' \"$GITSYNC_REPO_NAME\" \"$GITSYNC_CHANGED\" \"$PASSTHROUGH\" \"${GITSYNC_OIDC_CLIENT_SECRET-unset}\" \"$GITSYNC_TRIGGER\" \"$PWD\"", AccessUsername: "git"}
	var out bytes.Buffer
	result, err := e.Run(context.Background(), repo, Request{Trigger: model.TriggerWebhook, Source: model.SourceGitHub}, &out)
	if err != nil {
		t.Fatalf("post command: %v\n%s", err, out.String())
	}
	if result.FailedStage != "" || !strings.Contains(out.String(), "sample|true|copied|unset|webhook|"+repo.Path) {
		t.Fatalf("unexpected post env: %+v\n%s", result, out.String())
	}
	repo.Path = filepath.Join(t.TempDir(), "failed")
	repo.PostCommand = "exit 2"
	result, err = e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || result.FailedStage != model.StagePostCommand || result.Summary != "post command exited with 2" {
		t.Fatalf("failure result=%+v err=%v", result, err)
	}
}

func TestExecutorPostCommandTimeout(t *testing.T) {
	remote, _, bare := makeRemote(t)
	e := newTestExecutor(t, map[string]string{remote: bare})
	e.PostCommandTimeout = 100 * time.Millisecond
	repo := model.Repository{Name: "sample", URL: remote, Path: filepath.Join(t.TempDir(), "clone"), PostCommand: "sleep 5", AccessUsername: "git"}
	result, err := e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || result.FailedStage != model.StagePostCommand || !strings.Contains(result.Summary, "timed out") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func newTestExecutor(t *testing.T, remotes map[string]string) *Executor {
	t.Helper()
	e, err := NewExecutor(t.TempDir(), 10*time.Second, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	for remote, bare := range remotes {
		run(t, e.Home, "git", "config", "--file", filepath.Join(e.Home, ".gitconfig"), "url.file://"+bare+".insteadOf", remote)
	}
	run(t, e.Home, "git", "config", "--file", filepath.Join(e.Home, ".gitconfig"), "protocol.file.allow", "always")
	return e
}

func makeRemote(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bare, seed := filepath.Join(root, "remote.git"), filepath.Join(root, "seed")
	run(t, root, "git", "init", "--bare", "--initial-branch=main", bare)
	run(t, root, "git", "--git-dir", bare, "config", "http.receivepack", "true")
	run(t, root, "git", "init", "--initial-branch=main", seed)
	run(t, seed, "git", "config", "user.name", "Test")
	run(t, seed, "git", "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(seed, "README"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, seed, "git", "add", "README")
	run(t, seed, "git", "commit", "-m", "initial")
	run(t, seed, "git", "remote", "add", "origin", bare)
	run(t, seed, "git", "push", "-u", "origin", "main")
	return "https://phase4.invalid/" + filepath.Base(root) + ".git", seed, bare
}

func commit(t *testing.T, dir, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte(message+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "README")
	run(t, dir, "git", "commit", "-m", message)
	run(t, dir, "git", "push", "-u", "origin", "HEAD")
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", fmt.Sprint(args), err, output)
	}
}

func TestExecutorFastForwardFailureAndHardReset(t *testing.T) {
	remote, seed, bare := makeRemote(t)
	e := newTestExecutor(t, map[string]string{remote: bare})
	repo := model.Repository{Name: "sample", URL: remote, Path: filepath.Join(t.TempDir(), "clone"), AccessUsername: "git"}
	if _, err := e.Run(context.Background(), repo, Request{}, ioDiscard{}); err != nil {
		t.Fatal(err)
	}
	commitLocal(t, repo.Path, "local-only")
	commit(t, seed, "remote-only")
	result, err := e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || result.FailedStage != model.StageMerge || !strings.Contains(result.Summary, "merge failed") {
		t.Fatalf("expected ff-only failure, result=%+v err=%v", result, err)
	}
	repo.HardReset = true
	result, err = e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err != nil {
		t.Fatalf("hard reset: %v", err)
	}
	if result.FailedStage != "" || !result.Changed {
		t.Fatalf("unexpected reset result: %+v", result)
	}
	if got := gitValue(t, repo.Path, "rev-parse", "HEAD"); got != gitValue(t, seed, "rev-parse", "HEAD") {
		t.Fatalf("hard reset left %s, want remote %s", got, gitValue(t, seed, "rev-parse", "HEAD"))
	}
}

func TestExecutorDirtyBranchSwitchRequiresHardReset(t *testing.T) {
	remote, seed, bare := makeRemote(t)
	run(t, seed, "git", "checkout", "-b", "stable")
	commit(t, seed, "stable branch")
	run(t, seed, "git", "push", "-u", "origin", "stable")
	e := newTestExecutor(t, map[string]string{remote: bare})
	repo := model.Repository{Name: "sample", URL: remote, Path: filepath.Join(t.TempDir(), "clone"), AccessUsername: "git"}
	if _, err := e.Run(context.Background(), repo, Request{}, ioDiscard{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.Path, "README"), []byte("dirty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo.Branch = "stable"
	result, err := e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err == nil || result.FailedStage != model.StageCheckout {
		t.Fatalf("expected dirty checkout failure, result=%+v err=%v", result, err)
	}
	repo.HardReset = true
	result, err = e.Run(context.Background(), repo, Request{}, ioDiscard{})
	if err != nil {
		t.Fatalf("force branch switch: %v", err)
	}
	if result.ResolvedBranch != "stable" {
		t.Fatalf("resolved branch = %q", result.ResolvedBranch)
	}
}

func commitLocal(t *testing.T, dir, message string) {
	t.Helper()
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte(message+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "git", "add", "README")
	run(t, dir, "git", "commit", "-m", message)
}

func gitValue(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func TestGitEnvironmentReplacesAmbientConfig(t *testing.T) {
	e := newTestExecutor(t, nil)
	t.Setenv("GIT_CONFIG_COUNT", "4")
	t.Setenv("GIT_CONFIG_KEY_3", "http.*.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_3", "Authorization: Basic ambient-secret")
	t.Setenv("GIT_TRACE_CURL", "1")
	t.Setenv("GIT_CURL_VERBOSE", "1")
	repo := model.Repository{URL: "https://git.example:9443/team/repo.git"}
	pairs := e.gitEnv(repo)
	joined := strings.Join(pairs, "\n")
	for _, ambient := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_3", "GIT_CONFIG_VALUE_3", "GIT_TRACE_CURL", "GIT_CURL_VERBOSE"} {
		if strings.Contains(joined, ambient+"=") {
			t.Fatalf("ambient Git setting %s leaked into child env: %s", ambient, joined)
		}
	}
	if !strings.Contains(joined, "GIT_CONFIG_NOSYSTEM=1") {
		t.Fatalf("system Git config was not disabled: %s", joined)
	}
	repo.AccessToken, repo.AccessUsername = "secret-token", "alice"
	pairs = e.gitEnv(repo)
	joined = strings.Join(pairs, "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=1") || !strings.Contains(joined, "GIT_CONFIG_KEY_0=http.https://git.example:9443/.extraHeader") || strings.Contains(joined, "GIT_CONFIG_KEY_3") || strings.Contains(joined, "secret-token") {
		t.Fatalf("unexpected authenticated Git env: %s", joined)
	}

	program := filepath.Join(t.TempDir(), "fake-git")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nif [ -n \"${GIT_TRACE_CURL-}${GIT_CURL_VERBOSE-}\" ]; then printf '%s\\n%s\\n' \"${GIT_CONFIG_VALUE_0-}\" \"${GIT_CONFIG_VALUE_3-}\"; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := gitx.Run(context.Background(), gitx.Spec{Args: []string{program}, Env: pairs, Out: &output, Timeout: time.Second, Token: repo.AccessToken, Redact: e.gitSecrets(repo)}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Basic ") || strings.Contains(output.String(), "secret-token") || strings.Contains(output.String(), "ambient-secret") {
		t.Fatalf("credential exposed through ambient tracing: %q", output.String())
	}
}
