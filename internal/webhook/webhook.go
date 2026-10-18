// Package webhook handles authenticated Git provider webhook requests.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"gitsync/internal/model"
	"gitsync/internal/respond"
	"gitsync/internal/store"
	"gitsync/internal/syncer"
)

const MaxBodyBytes = 25 << 20

type repositoryStore interface {
	Get(context.Context, string) (*model.Repository, error)
}

type manager interface {
	Submit(context.Context, string, syncer.TriggerInfo) (model.Activity, error)
}

type Handler struct {
	globalSecret string
	repositories repositoryStore
	manager      manager
	logger       *slog.Logger
}

func NewHandler(globalSecret string, repositories repositoryStore, manager manager, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{globalSecret: globalSecret, repositories: repositories, manager: manager, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repositoryID := r.PathValue("repository_id")
	source := "unknown"
	outcome := "internal_error"
	defer func() {
		h.logger.Info("webhook request", "repository_id", repositoryID, "source", source, "outcome", outcome)
	}()

	repo, err := h.repositories.Get(r.Context(), repositoryID)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		outcome = "not_found"
		respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load repository")
		return
	}

	secret := repo.WebhookSecret
	if secret == "" {
		secret = h.globalSecret
	}
	if secret == "" {
		outcome = "secret_not_configured"
		respond.Error(w, http.StatusForbidden, "webhook_secret_not_configured", "webhook secret is not configured")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			outcome = "body_too_large"
			respond.Error(w, http.StatusRequestEntityTooLarge, "bad_request", "webhook body is too large")
			return
		}
		outcome = "body_read_error"
		respond.Error(w, http.StatusBadRequest, "bad_request", "could not read webhook body")
		return
	}

	sourceValue, authenticated := authenticate(r.Header, body, secret)
	if !authenticated {
		outcome = "unauthorized"
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "invalid webhook credentials")
		return
	}
	source = string(sourceValue)

	ignored, reason, err := filterEvent(sourceValue, r.Header, body, *repo)
	if err != nil {
		outcome = "bad_request"
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid webhook payload")
		return
	}
	if ignored {
		outcome = "ignored"
		respond.JSON(w, http.StatusOK, map[string]string{"status": "ignored", "reason": reason})
		return
	}

	activity, err := h.manager.Submit(r.Context(), repo.ID, syncer.TriggerInfo{Trigger: model.TriggerWebhook, Source: sourceValue})
	if errors.Is(err, syncer.ErrBusy) {
		outcome = "busy"
		respond.Error(w, http.StatusConflict, "conflict", "repository already has a queued or running sync")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not queue webhook sync")
		return
	}
	outcome = "accepted"
	respond.JSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "activity_id": activity.ID})
}

func authenticate(headers http.Header, body []byte, secret string) (model.Source, bool) {
	if values := headers.Values("X-Hub-Signature-256"); len(values) > 0 {
		if len(values) != 1 {
			return "", false
		}
		signature := values[0]
		if !strings.HasPrefix(signature, "sha256=") {
			return "", false
		}
		encoded := strings.TrimPrefix(signature, "sha256=")
		if len(encoded) != sha256.Size*2 {
			return "", false
		}
		for _, b := range encoded {
			if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
				return "", false
			}
		}
		got, err := hex.DecodeString(encoded)
		if err != nil {
			return "", false
		}
		mac := hmac.New(sha256.New, []byte(secret))
		if _, err := mac.Write(body); err != nil {
			return "", false
		}
		return model.SourceGitHub, hmac.Equal(got, mac.Sum(nil))
	}
	if values := headers.Values("X-Gitlab-Token"); len(values) > 0 {
		return model.SourceGitLab, len(values) == 1 && constantTimeEqual(values[0], secret)
	}
	if values := headers.Values("X-Token"); len(values) > 0 {
		return model.SourceOther, len(values) == 1 && constantTimeEqual(values[0], secret)
	}
	return "", false
}

func constantTimeEqual(left, right string) bool {
	return hmac.Equal([]byte(left), []byte(right))
}

func filterEvent(source model.Source, headers http.Header, body []byte, repo model.Repository) (bool, string, error) {
	switch source {
	case model.SourceGitHub:
		event := headers.Get("X-GitHub-Event")
		if event == "ping" {
			return true, "ping", nil
		}
		if event != "push" {
			return true, "event_not_supported", nil
		}
	case model.SourceGitLab:
		if headers.Get("X-Gitlab-Event") != "Push Hook" {
			return true, "event_not_supported", nil
		}
	case model.SourceOther:
		return false, "", nil
	default:
		return false, "", fmt.Errorf("unsupported webhook source %q", source)
	}

	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, "", fmt.Errorf("decode push payload: %w", err)
	}
	if strings.HasPrefix(payload.Ref, "refs/tags/") {
		return true, "tag", nil
	}

	trackedBranch := repo.Branch
	if trackedBranch == "" {
		trackedBranch = repo.ResolvedBranch
	}
	if trackedBranch != "" && strings.TrimPrefix(payload.Ref, "refs/heads/") != trackedBranch {
		return true, "branch_mismatch", nil
	}
	return false, "", nil
}
