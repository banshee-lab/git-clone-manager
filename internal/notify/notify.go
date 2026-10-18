package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gitsync/internal/config"
	"gitsync/internal/model"
)

const telegramAPIURL = "https://api.telegram.org"

// Notifier sends notifications for completed sync activities.
type Notifier interface {
	Notify(context.Context, model.Activity, model.Repository)
}

type logTailStore interface {
	Tail(repoID, activityID string, nLines int) ([]byte, error)
}

type channel interface {
	name() string
	send(context.Context, notification) (int, error)
}

type notification struct {
	Repository model.Repository
	Activity   model.Activity
	LogTail    string
	Link       string
}

// Multi sends notifications to each enabled channel concurrently.
type Multi struct {
	channels []channel
	logs     logTailStore
	baseURL  string
	timeout  time.Duration
	logger   *slog.Logger
	sleep    func(context.Context, time.Duration) error
}

// New builds a notifier using the enabled channels from cfg.
func New(cfg config.Config, logs logTailStore, logger *slog.Logger, version string) *Multi {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Multi{
		logs:    logs,
		baseURL: cfg.Server.BaseURL,
		timeout: time.Duration(cfg.Notifications.Webhook.Timeout),
		logger:  logger,
		sleep:   sleepContext,
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if cfg.Notifications.Telegram.Enabled {
		m.channels = append(m.channels, &telegram{
			token:   cfg.Notifications.Telegram.BotToken,
			chatID:  cfg.Notifications.Telegram.ChatID,
			baseURL: telegramAPIURL,
			client:  client,
		})
	}
	if cfg.Notifications.Webhook.Enabled {
		headers := make(http.Header, len(cfg.Notifications.Webhook.Headers))
		for key, value := range cfg.Notifications.Webhook.Headers {
			headers.Set(key, value)
		}
		m.channels = append(m.channels, &webhook{
			endpoint: cfg.Notifications.Webhook.URL,
			headers:  headers,
			secret:   cfg.Notifications.Webhook.Secret,
			version:  version,
			client:   client,
		})
	}
	return m
}

// Notify returns immediately; delivery work uses its own bounded context.
func (m *Multi) Notify(ctx context.Context, activity model.Activity, repo model.Repository) {
	if activity.Status != model.ActivityFailed && activity.Status != model.ActivityInterrupted {
		return
	}
	if len(m.channels) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		var tail string
		if m.logs != nil {
			data, err := m.logs.Tail(repo.ID, activity.ID, 40)
			if err != nil {
				m.logger.Warn("notification log tail unavailable", "channel", "log_tail", "status_code", 0)
			} else {
				tail = string(data)
			}
		}
		item := notification{
			Repository: repo,
			Activity:   activity,
			LogTail:    tail,
			Link:       activityLink(m.baseURL, repo.ID, activity.ID),
		}
		var workers sync.WaitGroup
		for _, destination := range m.channels {
			workers.Add(1)
			go func(destination channel) {
				defer workers.Done()
				m.deliver(ctx, destination, item)
			}(destination)
		}
		workers.Wait()
	}()
}

func (m *Multi) deliver(parent context.Context, destination channel, item notification) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), m.timeout)
	defer cancel()
	statusCode := 0
	var err error
	for attempt, delay := range []time.Duration{0, 2 * time.Second, 5 * time.Second} {
		if attempt > 0 {
			if err = m.sleep(ctx, delay); err != nil {
				break
			}
		}
		statusCode, err = destination.send(ctx, item)
		if err == nil && statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			return
		}
	}
	m.logger.Warn("notification delivery failed", "channel", destination.name(), "status_code", statusCode)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func activityLink(baseURL, repositoryID, activityID string) string {
	return strings.TrimRight(baseURL, "/") + "/repositories/" + url.PathEscape(repositoryID) + "?activity=" + url.QueryEscape(activityID)
}

type telegram struct {
	token   string
	chatID  string
	baseURL string
	client  *http.Client
}

func (t *telegram) name() string { return "telegram" }

func (t *telegram) send(ctx context.Context, item notification) (int, error) {
	message := telegramMessage(item)
	payload := struct {
		ChatID                string `json:"chat_id"`
		Text                  string `json:"text"`
		ParseMode             string `json:"parse_mode"`
		DisableWebPagePreview bool   `json:"disable_web_page_preview"`
	}{t.chatID, message, "HTML", true}
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, errors.New("telegram payload encoding failed")
	}
	target := strings.TrimRight(t.baseURL, "/") + "/bot" + t.token + "/sendMessage"
	status, err := postJSON(ctx, t.client, target, body, nil)
	if err != nil {
		if status != 0 {
			return status, fmt.Errorf("telegram request failed with HTTP %d", status)
		}
		return 0, errors.New("telegram request failed")
	}
	return status, nil
}

func telegramMessage(item notification) string {
	activity := item.Activity
	return "<b>Git sync " + html.EscapeString(string(activity.Status)) + "</b>\n" +
		"Repository: " + html.EscapeString(item.Repository.Name) + "\n" +
		"Status: " + html.EscapeString(string(activity.Status)) + "\n" +
		"Stage: " + html.EscapeString(string(activity.FailedStage)) + "\n" +
		"Summary: " + html.EscapeString(activity.Summary) + "\n" +
		"Trigger: " + html.EscapeString(string(activity.Trigger)) + "\n" +
		"Source: " + html.EscapeString(string(activity.Source)) + "\n" +
		"<a href=\"" + html.EscapeString(item.Link) + "\">View activity</a>"
}

type webhook struct {
	endpoint string
	headers  http.Header
	secret   string
	version  string
	client   *http.Client
}

func (w *webhook) name() string { return "webhook" }

func (w *webhook) send(ctx context.Context, item notification) (int, error) {
	activity := item.Activity
	body, err := json.Marshal(struct {
		Event      string `json:"event"`
		Repository struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			URL    string `json:"url"`
			Branch string `json:"branch"`
		} `json:"repository"`
		Activity struct {
			ID          string               `json:"id"`
			Status      model.ActivityStatus `json:"status"`
			Trigger     model.Trigger        `json:"trigger"`
			Source      model.Source         `json:"source"`
			FailedStage model.Stage          `json:"failed_stage"`
			Summary     string               `json:"summary"`
			StartedAt   *time.Time           `json:"started_at"`
			FinishedAt  *time.Time           `json:"finished_at"`
		} `json:"activity"`
		LogTail string `json:"log_tail"`
		Link    string `json:"link"`
	}{
		Event: "sync." + string(activity.Status),
		Repository: struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			URL    string `json:"url"`
			Branch string `json:"branch"`
		}{item.Repository.ID, item.Repository.Name, sanitizedRepositoryURL(item.Repository.URL), item.Repository.Branch},
		Activity: struct {
			ID          string               `json:"id"`
			Status      model.ActivityStatus `json:"status"`
			Trigger     model.Trigger        `json:"trigger"`
			Source      model.Source         `json:"source"`
			FailedStage model.Stage          `json:"failed_stage"`
			Summary     string               `json:"summary"`
			StartedAt   *time.Time           `json:"started_at"`
			FinishedAt  *time.Time           `json:"finished_at"`
		}{
			ID: activity.ID, Status: activity.Status, Trigger: activity.Trigger,
			Source: activity.Source, FailedStage: activity.FailedStage, Summary: activity.Summary,
			StartedAt: activity.StartedAt, FinishedAt: activity.FinishedAt,
		},
		LogTail: item.LogTail,
		Link:    item.Link,
	})
	if err != nil {
		return 0, errors.New("webhook payload encoding failed")
	}
	headers := w.headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("User-Agent", "gitsync/"+w.version)
	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		if _, err := mac.Write(body); err != nil {
			return 0, errors.New("webhook signature could not be computed")
		}
		headers.Set("X-GitSync-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	status, err := postJSON(ctx, w.client, w.endpoint, body, headers)
	if err != nil {
		if status != 0 {
			return status, fmt.Errorf("webhook request failed with HTTP %d", status)
		}
		return 0, errors.New("webhook request failed")
	}
	return status, nil
}

func sanitizedRepositoryURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	return parsed.String()
}

func postJSON(ctx context.Context, client *http.Client, target string, body []byte, headers http.Header) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("notification request could not be created")
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if response != nil {
		defer response.Body.Close()
		if err != nil {
			return response.StatusCode, errors.New("notification request failed")
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return response.StatusCode, fmt.Errorf("notification returned HTTP %d", response.StatusCode)
		}
		return response.StatusCode, nil
	}
	if err != nil {
		return 0, errors.New("notification request failed")
	}
	return 0, errors.New("notification request returned no response")
}

var _ Notifier = (*Multi)(nil)
