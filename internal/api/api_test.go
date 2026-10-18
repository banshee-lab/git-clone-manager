package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitsync/internal/config"
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

	if err := validateRepositoryInput(cfg, repos, ctx, base, true); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	badBranch := base
	badBranch.Branch = "bad branch"
	if err := validateRepositoryInput(cfg, repos, ctx, badBranch, true); err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("expected branch validation error, got %v", err)
	}

	badPath := base
	badPath.Path = cfg.Storage.DataDir
	if err := validateRepositoryInput(cfg, repos, ctx, badPath, true); err == nil || !strings.Contains(err.Error(), "data_dir") {
		t.Fatalf("expected data_dir validation error, got %v", err)
	}

	dupName := base
	dupName.Name = "alpha"
	seed := model.Repository{ID: "7e3a5d18-17f7-4b9f-a44d-8898bd2d2d97", Name: "alpha", URL: "https://example.com/seed.git", Path: filepath.Join(t.TempDir(), "other")}
	if err := repos.Create(ctx, &seed); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := validateRepositoryInput(cfg, repos, ctx, dupName, true); err == nil {
		t.Fatal("expected duplicate repository name conflict")
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
