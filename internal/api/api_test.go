package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitsync/internal/auth"
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

	credentialURL := base
	credentialURL.URL = "https://canary-user:canary-token@example.com/repo.git"
	if err := validateRepositoryInput(cfg, repos, ctx, credentialURL, true, ""); err == nil || !strings.Contains(err.Error(), "embedded credentials") {
		t.Fatalf("expected embedded URL credentials to be rejected, got %v", err)
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

	symlinkRoot := t.TempDir()
	symlinkPath := filepath.Join(symlinkRoot, "alias")
	if err := os.Symlink(t.TempDir(), symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	symlinked := base
	symlinked.Path = filepath.Join(symlinkPath, "repo")
	if err := validateRepositoryInput(cfg, repos, ctx, symlinked, true, ""); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlinked path to be rejected, got %v", err)
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

func TestRepositoryOutputOmitsAccessToken(t *testing.T) {
	const canary = "canary-access-token-do-not-return"
	repo := model.Repository{ID: "11111111-1111-4111-8111-111111111111", Name: "alpha", AccessToken: canary}
	body, err := json.Marshal(repositoryOut(repo, "https://example.com"))
	if err != nil {
		t.Fatalf("marshal repository: %v", err)
	}
	if strings.Contains(string(body), canary) || strings.Contains(string(body), `"access_token"`) {
		t.Fatalf("repository response exposed access token: %s", body)
	}
}

func TestGetRepositoryIncludesLatestAndRunningActivity(t *testing.T) {
	cfg := config.Default()
	repos, acts := store.NewMemory()
	repo := model.Repository{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "alpha", URL: "https://example.com/repo.git", Branch: "main", Path: "/tmp/repo-alpha", AccessUsername: "git", WebhookSecret: "secret"}
	if err := repos.Create(context.Background(), &repo); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	last := model.Activity{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivitySuccess, Trigger: model.TriggerManual, Source: model.SourceManual, TriggeredBy: "admin", Summary: "done", CreatedAt: time.Now().UTC()}
	if err := acts.Create(context.Background(), &last); err != nil {
		t.Fatalf("create last activity: %v", err)
	}
	running := model.Activity{ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", RepositoryID: repo.ID, RepositoryName: repo.Name, Status: model.ActivityRunning, Trigger: model.TriggerManual, Source: model.SourceManual, TriggeredBy: "admin", Summary: "running", CreatedAt: time.Now().UTC().Add(time.Minute)}
	if err := acts.Create(context.Background(), &running); err != nil {
		t.Fatalf("create running activity: %v", err)
	}
	service := &Service{cfg: cfg, repos: repos, activities: acts}
	req := httptest.NewRequest("GET", "/api/repositories/"+repo.ID, nil)
	req.SetPathValue("id", repo.ID)
	user := auth.User{Sub: "user-1", Email: "admin@example.test", Name: "Admin User", Role: "admin"}
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	service.GetRepository(rec, req)
	if rec.Code != 200 {
		t.Fatalf("get repository status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got Repository
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode repository: %v", err)
	}
	if got.LastActivity == nil || got.LastActivity.ID != last.ID {
		t.Fatalf("last_activity missing or wrong: %+v", got.LastActivity)
	}
	if got.RunningActivityID != running.ID {
		t.Fatalf("running_activity_id missing or wrong: got %q want %q", got.RunningActivityID, running.ID)
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
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(baseDir, alias); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := validateDeletePath(dataDir, filepath.Join(alias, "repo")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlinked delete path to fail, got %v", err)
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
