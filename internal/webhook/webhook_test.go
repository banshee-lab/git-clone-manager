package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitsync/internal/model"
	"gitsync/internal/store"
	"gitsync/internal/syncer"
)

const testRepositoryID = "7e3a5d18-17f7-4b9f-a44d-8898bd2d2d97"

type fakeManager struct {
	activity model.Activity
	info     syncer.TriggerInfo
	err      error
	calls    int
}

func (m *fakeManager) Submit(_ context.Context, repoID string, info syncer.TriggerInfo) (model.Activity, error) {
	m.calls++
	m.info = info
	m.activity.RepositoryID = repoID
	return m.activity, m.err
}

func TestHandlerAuthentication(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	tests := []struct {
		name       string
		provider   string
		repoSecret string
		global     string
		valid      bool
		mutate     func(http.Header, []byte)
		wantSource model.Source
	}{
		{name: "github repository secret", provider: "github", repoSecret: "repo-secret", global: "global-secret", valid: true, wantSource: model.SourceGitHub},
		{name: "github global fallback", provider: "github", global: "global-secret", valid: true, wantSource: model.SourceGitHub},
		{name: "github wrong repository secret", provider: "github", repoSecret: "repo-secret", global: "global-secret", mutate: func(h http.Header, b []byte) { setGitHubSignature(h, b, "global-secret") }},
		{name: "github wrong body", provider: "github", repoSecret: "repo-secret", valid: false, mutate: func(h http.Header, _ []byte) {
			h.Set("X-Hub-Signature-256", githubSignature("repo-secret", []byte("different body")))
		}},
		{name: "github missing prefix", provider: "github", repoSecret: "repo-secret", mutate: func(h http.Header, b []byte) {
			h.Set("X-Hub-Signature-256", strings.TrimPrefix(githubSignature("repo-secret", b), "sha256="))
		}},
		{name: "github uppercase hex", provider: "github", repoSecret: "repo-secret", mutate: func(h http.Header, b []byte) {
			signature := githubSignature("repo-secret", b)
			h.Set("X-Hub-Signature-256", "sha256="+strings.ToUpper(strings.TrimPrefix(signature, "sha256=")))
		}},
		{name: "gitlab repository secret", provider: "gitlab", repoSecret: "repo-secret", global: "global-secret", valid: true, wantSource: model.SourceGitLab},
		{name: "gitlab global fallback", provider: "gitlab", global: "global-secret", valid: true, wantSource: model.SourceGitLab},
		{name: "gitlab repository secret takes precedence", provider: "gitlab", repoSecret: "repo-secret", global: "global-secret", valid: false, mutate: func(h http.Header, _ []byte) { h.Set("X-Gitlab-Token", "global-secret") }},
		{name: "generic repository secret", provider: "other", repoSecret: "repo-secret", global: "global-secret", valid: true, wantSource: model.SourceOther},
		{name: "generic global fallback", provider: "other", global: "global-secret", valid: true, wantSource: model.SourceOther},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secret := test.repoSecret
			if secret == "" {
				secret = test.global
			}
			manager := &fakeManager{activity: model.Activity{ID: "activity-1"}}
			handler, _ := newTestHandler(t, test.repoSecret, test.global, manager)
			request := webhookRequest(body)
			setAuthHeader(request.Header, test.provider, secret, body)
			if test.mutate != nil {
				test.mutate(request.Header, body)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if test.valid {
				if response.Code != http.StatusAccepted {
					t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
				}
				if manager.calls != 1 || manager.info.Source != test.wantSource || manager.info.Trigger != model.TriggerWebhook {
					t.Fatalf("submit calls=%d info=%+v", manager.calls, manager.info)
				}
			} else if response.Code != http.StatusUnauthorized || manager.calls != 0 {
				t.Fatalf("invalid credentials status=%d calls=%d body=%s", response.Code, manager.calls, response.Body.String())
			}
		})
	}
}

func TestHandlerAuthFailureAndHeaderPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		repoSecret string
		global     string
		headers    func(http.Header, []byte)
		wantStatus int
	}{
		{name: "unknown repository", repoSecret: "secret", global: "global", wantStatus: http.StatusNotFound},
		{name: "no configured secret", wantStatus: http.StatusForbidden},
		{name: "no auth header", repoSecret: "secret", wantStatus: http.StatusUnauthorized},
		{
			name:       "invalid github does not fall through",
			repoSecret: "secret",
			headers: func(h http.Header, b []byte) {
				h.Set("X-Hub-Signature-256", githubSignature("wrong", b))
				h.Set("X-Gitlab-Token", "secret")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid gitlab does not fall through",
			repoSecret: "secret",
			headers: func(h http.Header, _ []byte) {
				h.Set("X-Gitlab-Token", "wrong")
				h.Set("X-Token", "secret")
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "github has priority when multiple valid headers are present",
			repoSecret: "secret",
			headers: func(h http.Header, b []byte) {
				h.Set("X-Hub-Signature-256", githubSignature("secret", b))
				h.Set("X-GitHub-Event", "push")
				h.Set("X-Gitlab-Token", "secret")
				h.Set("X-Token", "secret")
			},
			wantStatus: http.StatusAccepted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := &fakeManager{activity: model.Activity{ID: "activity-1"}}
			handler, repos := newTestHandler(t, test.repoSecret, test.global, manager)
			if test.name == "unknown repository" {
				repos = nil
			}
			body := []byte(`{"ref":"refs/heads/main"}`)
			request := webhookRequest(body)
			if test.headers != nil {
				test.headers(request.Header, body)
			}
			response := httptest.NewRecorder()
			if repos == nil {
				handler = NewHandler(test.global, emptyRepositoryStore{}, manager, nil)
			}
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.name == "github has priority when multiple valid headers are present" && manager.info.Source != model.SourceGitHub {
				t.Fatalf("source = %q, want github", manager.info.Source)
			}
		})
	}
}

func TestHandlerEventFiltering(t *testing.T) {
	tests := []struct {
		name           string
		source         string
		event          string
		ref            string
		branch         string
		resolvedBranch string
		body           string
		wantStatus     int
		wantIgnored    string
	}{
		{name: "github ping", source: "github", event: "ping", body: `{}`, wantStatus: http.StatusOK, wantIgnored: "ping"},
		{name: "github other event", source: "github", event: "issues", body: `{}`, wantStatus: http.StatusOK, wantIgnored: "event_not_supported"},
		{name: "github tag", source: "github", event: "push", ref: "refs/tags/v1", branch: "main", wantStatus: http.StatusOK, wantIgnored: "tag"},
		{name: "github other branch", source: "github", event: "push", ref: "refs/heads/feature", branch: "main", wantStatus: http.StatusOK, wantIgnored: "branch_mismatch"},
		{name: "github matching branch", source: "github", event: "push", ref: "refs/heads/main", branch: "main", wantStatus: http.StatusAccepted},
		{name: "gitlab tag push hook", source: "gitlab", event: "Tag Push Hook", ref: "refs/tags/v1", branch: "main", wantStatus: http.StatusOK, wantIgnored: "event_not_supported"},
		{name: "gitlab push uses resolved branch", source: "gitlab", event: "Push Hook", ref: "refs/heads/release", resolvedBranch: "release", wantStatus: http.StatusAccepted},
		{name: "configured branch takes precedence", source: "github", event: "push", ref: "refs/heads/release", branch: "main", resolvedBranch: "release", wantStatus: http.StatusOK, wantIgnored: "branch_mismatch"},
		{name: "untracked branch accepts", source: "github", event: "push", ref: "refs/heads/any", wantStatus: http.StatusAccepted},
		{name: "generic source ignores body", source: "other", body: "not json", wantStatus: http.StatusAccepted},
		{name: "invalid GitHub JSON", source: "github", event: "push", body: `{"ref":`, wantStatus: http.StatusBadRequest},
		{name: "invalid GitLab JSON", source: "gitlab", event: "Push Hook", body: `{"ref":`, wantStatus: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(test.body)
			if test.body == "" {
				body, _ = json.Marshal(map[string]string{"ref": test.ref})
			}
			manager := &fakeManager{activity: model.Activity{ID: "activity-1"}}
			handler, repos := newTestHandler(t, "secret", "", manager)
			repo, err := repos.Get(context.Background(), testRepositoryID)
			if err != nil {
				t.Fatal(err)
			}
			repo.Branch = test.branch
			repo.ResolvedBranch = test.resolvedBranch
			if err := repos.Update(context.Background(), repo); err != nil {
				t.Fatal(err)
			}
			request := webhookRequest(body)
			setAuthHeader(request.Header, test.source, "secret", body)
			if test.event != "" {
				switch test.source {
				case "github":
					request.Header.Set("X-GitHub-Event", test.event)
				case "gitlab":
					request.Header.Set("X-Gitlab-Event", test.event)
				}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantIgnored != "" {
				if !strings.Contains(response.Body.String(), `"reason":"`+test.wantIgnored+`"`) || manager.calls != 0 {
					t.Fatalf("ignored response=%s submit calls=%d", response.Body.String(), manager.calls)
				}
			}
			if test.wantStatus == http.StatusAccepted && (manager.calls != 1 || manager.info.Source != model.Source(test.source)) {
				t.Fatalf("submit calls=%d info=%+v", manager.calls, manager.info)
			}
		})
	}
}

func TestHandlerProviderSamplePayloads(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		source   string
		event    string
	}{
		{name: "GitHub push", filename: "github-push.json", source: "github", event: "push"},
		{name: "GitLab push", filename: "gitlab-push.json", source: "gitlab", event: "Push Hook"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join("testdata", test.filename))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			manager := &fakeManager{activity: model.Activity{ID: "sample-activity"}}
			handler, _ := newTestHandler(t, "secret", "", manager)
			request := webhookRequest(body)
			setAuthHeader(request.Header, test.source, "secret", body)
			if test.source == "github" {
				request.Header.Set("X-GitHub-Event", test.event)
			} else {
				request.Header.Set("X-Gitlab-Event", test.event)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusAccepted || manager.calls != 1 || manager.info.Source != model.Source(test.source) {
				t.Fatalf("status=%d submits=%d source=%q body=%s", response.Code, manager.calls, manager.info.Source, response.Body.String())
			}
		})
	}
}

func TestHandlerBodyLimitBusyAndAcceptedResponse(t *testing.T) {
	t.Run("oversized body", func(t *testing.T) {
		manager := &fakeManager{}
		handler, _ := newTestHandler(t, "secret", "", manager)
		body := bytes.Repeat([]byte("x"), MaxBodyBytes+1)
		request := webhookRequest(body)
		request.Header.Set("X-Token", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge || manager.calls != 0 {
			t.Fatalf("status=%d calls=%d", response.Code, manager.calls)
		}
	})

	t.Run("busy repository", func(t *testing.T) {
		manager := &fakeManager{err: syncer.ErrBusy}
		handler, _ := newTestHandler(t, "secret", "", manager)
		request := webhookRequest([]byte("ignored"))
		request.Header.Set("X-Token", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("accepted response contains activity ID", func(t *testing.T) {
		manager := &fakeManager{activity: model.Activity{ID: "created-activity"}}
		handler, _ := newTestHandler(t, "secret", "", manager)
		request := webhookRequest([]byte("ignored"))
		request.Header.Set("X-Token", "secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"activity_id":"created-activity"`) {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}

func TestHandlerLogsOutcomeWithoutPayload(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	repos, _ := store.NewMemory()
	repo := model.Repository{ID: testRepositoryID, Name: "demo", WebhookSecret: "secret"}
	if err := repos.Create(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}
	handler := NewHandler("", repos, manager, logger)
	body := []byte(`{"private":"payload-data"}`)
	request := webhookRequest(body)
	request.Header.Set("X-Token", "secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	logOutput := logs.String()
	if !strings.Contains(logOutput, `repository_id=`+testRepositoryID) ||
		!strings.Contains(logOutput, "source=other") ||
		!strings.Contains(logOutput, "outcome=accepted") ||
		strings.Contains(logOutput, "payload-data") {
		t.Fatalf("unexpected webhook log: %s", logOutput)
	}
}

type emptyRepositoryStore struct{}

func (emptyRepositoryStore) Get(context.Context, string) (*model.Repository, error) {
	return nil, store.ErrNotFound
}

func newTestHandler(t *testing.T, repoSecret, globalSecret string, manager *fakeManager) (*Handler, *store.MemoryRepositories) {
	t.Helper()
	repos, _ := store.NewMemory()
	repo := model.Repository{ID: testRepositoryID, Name: "demo", WebhookSecret: repoSecret, Branch: "main"}
	if err := repos.Create(context.Background(), &repo); err != nil {
		t.Fatalf("create test repository: %v", err)
	}
	return NewHandler(globalSecret, repos, manager, nil), repos
}

func webhookRequest(body []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/hooks/"+testRepositoryID, bytes.NewReader(body))
	request.SetPathValue("repository_id", testRepositoryID)
	return request
}

func setAuthHeader(headers http.Header, provider, secret string, body []byte) {
	switch provider {
	case "github":
		headers.Set("X-Hub-Signature-256", githubSignature(secret, body))
		headers.Set("X-GitHub-Event", "push")
	case "gitlab":
		headers.Set("X-Gitlab-Token", secret)
		headers.Set("X-Gitlab-Event", "Push Hook")
	case "other":
		headers.Set("X-Token", secret)
	}
}

func setGitHubSignature(headers http.Header, body []byte, secret string) {
	headers.Set("X-Hub-Signature-256", githubSignature(secret, body))
}

func githubSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(body); err != nil {
		panic(fmt.Sprintf("write HMAC input: %v", err))
	}
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
