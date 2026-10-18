package notify

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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gitsync/internal/config"
	"gitsync/internal/model"
)

const (
	testRepositoryID = "7e3a5d18-17f7-4b9f-a44d-8898bd2d2d97"
	testActivityID   = "79654727-1c90-4ab5-b93c-bdd5aa4fd6fb"
)

func testNotification(status model.ActivityStatus) notification {
	started := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	finished := started.Add(time.Minute)
	return notification{
		Repository: model.Repository{
			ID: testRepositoryID, Name: "repo <&>", URL: "https://git.example/repo.git", Branch: "main",
		},
		Activity: model.Activity{
			ID: testActivityID, RepositoryID: testRepositoryID, RepositoryName: "repo <&>",
			Status: status, Trigger: model.TriggerWebhook, Source: model.SourceGitHub,
			FailedStage: model.StageMerge, Summary: "merge <&> failed",
			StartedAt: &started, FinishedAt: &finished,
		},
		Link: "https://app.example/repositories/" + testRepositoryID + "?activity=" + testActivityID,
	}
}

func TestTelegramRequestEscapingAndSecretSafeFailure(t *testing.T) {
	t.Run("request shape and HTML escaping", func(t *testing.T) {
		requests := make(chan struct {
			path string
			body map[string]any
		}, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %q, want POST", r.Method)
			}
			if got := r.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			requests <- struct {
				path string
				body map[string]any
			}{r.URL.Path, body}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		sender := &telegram{token: "bot-secret", chatID: "chat-1", baseURL: server.URL, client: server.Client()}
		status, err := sender.send(context.Background(), testNotification(model.ActivityFailed))
		if err != nil || status != http.StatusOK {
			t.Fatalf("send = (%d, %v), want (200, nil)", status, err)
		}
		got := <-requests
		if got.path != "/botbot-secret/sendMessage" {
			t.Fatalf("path = %q", got.path)
		}
		if got.body["chat_id"] != "chat-1" || got.body["parse_mode"] != "HTML" || got.body["disable_web_page_preview"] != true {
			t.Fatalf("request fields = %#v", got.body)
		}
		message, ok := got.body["text"].(string)
		if !ok {
			t.Fatalf("text has type %T", got.body["text"])
		}
		for _, escaped := range []string{"repo &lt;&amp;&gt;", "merge &lt;&amp;&gt; failed", `href="https://app.example/repositories/` + testRepositoryID} {
			if !strings.Contains(message, escaped) {
				t.Errorf("message %q does not contain %q", message, escaped)
			}
		}
		if strings.Contains(message, "repo <&>") || strings.Contains(message, "merge <&>") {
			t.Fatalf("message contains unescaped HTML: %q", message)
		}
	})

	t.Run("failure does not expose token or response body", func(t *testing.T) {
		const token = "do-not-log-this-token"
		const responseSecret = "private response body"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := fmt.Fprint(w, responseSecret); err != nil {
				t.Errorf("write response body: %v", err)
			}
		}))
		defer server.Close()

		var logs bytes.Buffer
		m := &Multi{logger: slog.New(slog.NewTextHandler(&logs, nil)), timeout: time.Second, sleep: func(context.Context, time.Duration) error { return nil }}
		sender := &telegram{token: token, chatID: "chat", baseURL: server.URL, client: server.Client()}
		m.deliver(context.Background(), sender, testNotification(model.ActivityFailed))
		_, sendErr := sender.send(context.Background(), testNotification(model.ActivityFailed))
		if sendErr == nil || strings.Contains(sendErr.Error(), token) || strings.Contains(sendErr.Error(), responseSecret) {
			t.Fatalf("sender error exposed secret or was nil: %v", sendErr)
		}
		if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), responseSecret) {
			t.Fatalf("failure log exposed sensitive data: %q", logs.String())
		}
		if !strings.Contains(logs.String(), "status_code=500") {
			t.Fatalf("failure log omitted HTTP status: %q", logs.String())
		}
	})
}

func TestWebhookPayloadHeadersAndSignature(t *testing.T) {
	const secret = "signing-secret"
	item := testNotification(model.ActivityInterrupted)
	item.LogTail = "last 40 lines"
	item.Repository.URL = "https://repo-user:repo-password@git.example/repo.git"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "gitsync/v9-test" {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("X-Configured"); got != "Bearer test-value" {
			t.Errorf("configured header = %q", got)
		}
		body := new(bytes.Buffer)
		if _, err := body.ReadFrom(r.Body); err != nil {
			t.Errorf("read body: %v", err)
		}
		if strings.Contains(body.String(), "repo-user") || strings.Contains(body.String(), "repo-password") {
			t.Errorf("webhook payload exposed repository URL credentials: %s", body.String())
		}
		mac := hmac.New(sha256.New, []byte(secret))
		if _, err := mac.Write(body.Bytes()); err != nil {
			t.Errorf("compute expected signature: %v", err)
		}
		wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := r.Header.Get("X-GitSync-Signature"); !hmac.Equal([]byte(got), []byte(wantSignature)) {
			t.Errorf("signature = %q, want %q", got, wantSignature)
		}
		var payload map[string]any
		if err := json.Unmarshal(body.Bytes(), &payload); err != nil {
			t.Errorf("decode payload: %v", err)
		} else {
			assertWebhookPayload(t, payload, item)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := &webhook{
		endpoint: server.URL,
		headers:  http.Header{"X-Configured": []string{"Bearer test-value"}},
		secret:   secret,
		version:  "v9-test",
		client:   server.Client(),
	}
	status, err := sender.send(context.Background(), item)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("send = (%d, %v), want (204, nil)", status, err)
	}
}

func assertWebhookPayload(t *testing.T, payload map[string]any, item notification) {
	t.Helper()
	if payload["event"] != "sync.interrupted" || payload["log_tail"] != item.LogTail || payload["link"] != item.Link {
		t.Fatalf("payload root = %#v", payload)
	}
	repository, ok := payload["repository"].(map[string]any)
	if !ok || repository["id"] != item.Repository.ID || repository["name"] != item.Repository.Name ||
		repository["url"] != "https://git.example/repo.git" || repository["branch"] != item.Repository.Branch {
		t.Fatalf("repository payload = %#v", payload["repository"])
	}
	activity, ok := payload["activity"].(map[string]any)
	if !ok {
		t.Fatalf("activity payload = %#v", payload["activity"])
	}
	for key, want := range map[string]any{
		"id": item.Activity.ID, "status": "interrupted", "trigger": "webhook", "source": "github",
		"failed_stage": "merge", "summary": item.Activity.Summary,
		"started_at":  item.Activity.StartedAt.Format(time.RFC3339Nano),
		"finished_at": item.Activity.FinishedAt.Format(time.RFC3339Nano),
	} {
		if activity[key] != want {
			t.Errorf("activity.%s = %#v, want %#v", key, activity[key], want)
		}
	}
}

func TestConfiguredEnvironmentHeaderReachesWebhook(t *testing.T) {
	t.Setenv("NOTIFY_TEST_HEADER", "Bearer expanded-value")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer expanded-value" {
			t.Errorf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configData := fmt.Sprintf(`server:
  base_url: https://app.example
oidc:
  issuer_url: https://idp.example
  client_id: test-client
  client_secret: test-secret
  session_secret: 12345678901234567890123456789012
notifications:
  webhook:
    enabled: true
    url: %q
    headers:
      Authorization: "${NOTIFY_TEST_HEADER}"
`, server.URL)
	if err := os.WriteFile(configPath, []byte(configData), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	m := New(cfg, nil, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "test")
	status, err := m.channels[0].send(context.Background(), testNotification(model.ActivityFailed))
	if err != nil || status != http.StatusAccepted {
		t.Fatalf("send = (%d, %v), want (202, nil)", status, err)
	}
}

func TestRetryPolicy(t *testing.T) {
	tests := []struct {
		name       string
		responses  []int
		wantStatus int
		wantCalls  int
		wantDelays []time.Duration
	}{
		{
			name:       "retry twice then succeed",
			responses:  []int{http.StatusInternalServerError, http.StatusInternalServerError, http.StatusOK},
			wantStatus: http.StatusOK, wantCalls: 3,
			wantDelays: []time.Duration{2 * time.Second, 5 * time.Second},
		},
		{
			name:       "stop after three permanent failures",
			responses:  []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable},
			wantStatus: http.StatusServiceUnavailable, wantCalls: 3,
			wantDelays: []time.Duration{2 * time.Second, 5 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := tt.responses[calls.Add(1)-1]
				w.WriteHeader(status)
			}))
			defer server.Close()

			var delays []time.Duration
			m := &Multi{
				logger:  slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
				timeout: time.Second,
				sleep: func(_ context.Context, delay time.Duration) error {
					delays = append(delays, delay)
					return nil
				},
			}
			destination := &webhook{endpoint: server.URL, client: server.Client()}
			m.deliver(context.Background(), destination, testNotification(model.ActivityFailed))
			if calls.Load() != int32(tt.wantCalls) {
				t.Fatalf("request count = %d, want %d", calls.Load(), tt.wantCalls)
			}
			if len(delays) != len(tt.wantDelays) {
				t.Fatalf("delays = %v, want %v", delays, tt.wantDelays)
			}
			for i := range delays {
				if delays[i] != tt.wantDelays[i] {
					t.Errorf("delay[%d] = %s, want %s", i, delays[i], tt.wantDelays[i])
				}
			}
		})
	}
}

func TestMultiFailureDoesNotAffectOtherChannel(t *testing.T) {
	requests := make(chan string, 4)
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- "telegram"
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer telegramServer.Close()
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- "webhook"
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhookServer.Close()

	cfg := config.Default()
	cfg.Server.BaseURL = "https://app.example"
	cfg.Notifications.Telegram.Enabled = true
	cfg.Notifications.Telegram.BotToken = "token"
	cfg.Notifications.Telegram.ChatID = "chat"
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = webhookServer.URL
	cfg.Notifications.Webhook.Timeout = config.Duration(time.Second)
	m := New(cfg, nil, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "test")
	m.channels[0].(*telegram).baseURL = telegramServer.URL
	m.sleep = func(context.Context, time.Duration) error { return nil }

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	m.Notify(context.Background(), testNotification(model.ActivitySuccess).Activity, testNotification(model.ActivitySuccess).Repository)
	select {
	case got := <-requests:
		t.Fatalf("success activity was notified on %s", got)
	case <-time.After(30 * time.Millisecond):
	}
	item := testNotification(model.ActivityFailed)
	m.Notify(canceled, item.Activity, item.Repository)

	counts := map[string]int{}
	for range 4 {
		select {
		case got := <-requests:
			counts[got]++
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for all attempts; counts=%v", counts)
		}
	}
	if counts["telegram"] != 3 || counts["webhook"] != 1 {
		t.Fatalf("channel attempts = %v, want telegram=3 webhook=1", counts)
	}
}

type tailFixture struct {
	mu    sync.Mutex
	calls int
	lines int
}

func (f *tailFixture) Tail(_, _ string, lines int) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.lines = lines
	f.mu.Unlock()
	return []byte("last 40 redacted lines"), nil
}

func TestInterruptedNotificationUsesFortyLineLogTailAndDoesNotBlock(t *testing.T) {
	entered := make(chan map[string]any, 1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode notification: %v", err)
		}
		entered <- payload
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)
	cfg := config.Default()
	cfg.Server.BaseURL = "https://app.example"
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = server.URL
	cfg.Notifications.Webhook.Timeout = config.Duration(2 * time.Second)
	tail := &tailFixture{}
	m := New(cfg, tail, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "test")
	item := testNotification(model.ActivityInterrupted)

	start := time.Now()
	m.Notify(context.Background(), item.Activity, item.Repository)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Notify blocked for %s", elapsed)
	}
	select {
	case payload := <-entered:
		if payload["event"] != "sync.interrupted" || payload["log_tail"] != "last 40 redacted lines" {
			t.Fatalf("notification payload = %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("notification was not sent")
	}
	tail.mu.Lock()
	defer tail.mu.Unlock()
	if tail.calls != 1 || tail.lines != 40 {
		t.Fatalf("tail calls=%d lines=%d", tail.calls, tail.lines)
	}
}

func TestNotificationTimeoutStopsRetrySleep(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	m := &Multi{
		logger:  slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		timeout: 10 * time.Millisecond,
		sleep: func(ctx context.Context, _ time.Duration) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	destination := &webhook{endpoint: server.URL, client: server.Client()}
	m.deliver(context.Background(), destination, testNotification(model.ActivityFailed))
	if calls.Load() != 1 {
		t.Fatalf("request count = %d, want 1 after retry context timeout", calls.Load())
	}
}
