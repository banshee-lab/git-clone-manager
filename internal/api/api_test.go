package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitsync/internal/config"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/store"
)

func TestValidateRepositoryInputRules(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	repos, _ := store.NewMemory()
	ctx := context.Background()

	base := RepositoryInput{
		Name:                      "alpha",
		URL:                       "https://example.com/repo.git",
		Branch:                    "main",
		Path:                      filepath.Join(t.TempDir(), "repo"),
		AccessUsername:            "git",
		PostCommandTimeoutSeconds: 30,
	}

	if err := validateRepositoryInput(cfg, repos, ctx, base, true, ""); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	badBranch := base
	badBranch.Branch = "bad branch"
	if err := validateRepositoryInput(cfg, repos, ctx, badBranch, true, ""); err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected branch validation error, got %v", err)
	}

	badPath := base
	badPath.Path = cfg.Storage.DataDir
	if err := validateRepositoryInput(cfg, repos, ctx, badPath, true, ""); err == nil || !strings.Contains(err.Error(), "data_dir") {
		t.Fatalf("expected data_dir validation error, got %v", err)
	}

	dupName := base
	dupName.Name = "alpha"
	seed := model.Repository{ID: "7e3a5d18-17f7-4b9f-a44d-8898bd2d2d97", Name: "alpha", URL: "https://example.com/seed.git", Path: filepath.Join(t.TempDir(), "other")}
	if err := repos.Create(ctx, &seed); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := validateRepositoryInput(cfg, repos, ctx, dupName, true, ""); err == nil {
		t.Fatal("expected duplicate repository name conflict")
	}
}

func TestApplyRepositoryUpdatePreservesOmittedValues(t *testing.T) {
	existing := model.Repository{
		ID:                        "11111111-1111-4111-8111-111111111111",
		Name:                      "alpha",
		URL:                       "https://example.com/alpha.git",
		Branch:                    "main",
		Path:                      "/tmp/repo",
		AccessUsername:            "git",
		AccessToken:               "old-token",
		PostCommand:               "echo hello",
		PostCommandTimeoutSeconds: 15,
		HardReset:                 true,
		WebhookSecret:             "old-secret",
	}

	input, present, err := decodeRepositoryInputBody(strings.NewReader(`{"branch":"release","path":"/tmp/repo-2"}`))
	if err != nil {
		t.Fatalf("decode update body: %v", err)
	}
	updated := applyRepositoryUpdate(existing, input, present)

	if updated.Branch != "release" {
		t.Fatalf("branch not updated: got %q", updated.Branch)
	}
	if updated.Path != "/tmp/repo-2" {
		t.Fatalf("path not updated: got %q", updated.Path)
	}
	if updated.PostCommand != existing.PostCommand {
		t.Fatalf("post command overwritten: got %q want %q", updated.PostCommand, existing.PostCommand)
	}
	if updated.PostCommandTimeoutSeconds != existing.PostCommandTimeoutSeconds {
		t.Fatalf("timeout overwritten: got %d want %d", updated.PostCommandTimeoutSeconds, existing.PostCommandTimeoutSeconds)
	}
	if updated.HardReset != existing.HardReset {
		t.Fatalf("hard_reset overwritten: got %t want %t", updated.HardReset, existing.HardReset)
	}
	if updated.AccessToken != existing.AccessToken {
		t.Fatalf("access token overwritten: got %q want %q", updated.AccessToken, existing.AccessToken)
	}
}

func TestRepositoryOutputIncludesWebhookSecret(t *testing.T) {
	repo := model.Repository{ID: "11111111-1111-4111-8111-111111111111", Name: "alpha", WebhookSecret: "super-secret"}
	out := repositoryOut(repo, "https://example.com")
	if out.WebhookSecret != "super-secret" {
		t.Fatalf("secret omitted from repository payload: got %q want %q", out.WebhookSecret, "super-secret")
	}
}

func TestFinishedActivityStreamSkipsSnapshotOnResume(t *testing.T) {
	logs, err := logstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("new logstore: %v", err)
	}
	act := &model.Activity{ID: "44f3bd2a-ba64-4d2d-8e6f-111111111111", RepositoryID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Status: model.ActivitySuccess}
	if err := logs.Append(act.RepositoryID, act.ID, "hello world"); err != nil {
		t.Fatalf("append log: %v", err)
	}

	svc := &Service{logs: logs}
	rec := httptest.NewRecorder()
	w := respondingWriter{ResponseWriter: rec, Flusher: rec}
	svc.writeActivityStream(w, act, 5, context.Background())
	body := rec.Body.String()

	if strings.Contains(body, "event: snapshot") {
		t.Fatalf("resume offset should skip snapshot, body=%q", body)
	}
	if !strings.Contains(body, "event: log") {
		t.Fatalf("expected resumed log event, body=%q", body)
	}
	if !strings.Contains(body, "\"offset\":11") {
		t.Fatalf("expected final offset in resumed log payload, body=%q", body)
	}
}

func TestValidateDeletePathSafety(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	baseDir := filepath.Join(t.TempDir(), "repos")
	repoDir := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := validateDeletePath(dataDir, repoDir); err != nil {
		t.Fatalf("expected safe delete path to pass, got %v", err)
	}
	if err := validateDeletePath(dataDir, filepath.Join(baseDir, "sub", "repo")); err == nil {
		t.Fatal("expected nested path without .git to fail")
	}
	if err := validateDeletePath(dataDir, filepath.Join(baseDir, "safe")); err == nil {
		t.Fatal("expected path without .git to fail")
	}
}

func TestWriteSSEEventIncludesID(t *testing.T) {
	buf := httptest.NewRecorder()
	if err := writeSSEEventID(buf, buf, "log", "42", map[string]string{"status": "ok"}); err != nil {
		t.Fatalf("write SSE event: %v", err)
	}
	body := buf.Body.String()
	if !strings.Contains(body, "id: 42\n") || !strings.Contains(body, "event: log\n") {
		t.Fatalf("expected event id and kind in SSE payload, got %q", body)
	}
}
