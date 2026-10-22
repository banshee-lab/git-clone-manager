package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gitsync/internal/auth"
	"gitsync/internal/config"
	"gitsync/internal/events"
	"gitsync/internal/logstore"
	"gitsync/internal/model"
	"gitsync/internal/respond"
	"gitsync/internal/store"
	"gitsync/internal/syncer"
)

type Manager interface {
	Submit(context.Context, string, syncer.TriggerInfo) (model.Activity, error)
	SubmitAll(context.Context, syncer.TriggerInfo) ([]model.Activity, []syncer.Skip, error)
	IsBusy(string) bool
	LiveLog(string) *syncer.LiveLog
}

type Service struct {
	cfg        config.Config
	repos      store.Repositories
	activities store.Activities
	manager    Manager
	hub        *events.Hub
	logs       logstore.Store
	cleaner    interface {
		DeleteRepository(context.Context, string) error
	}
}

type RepositoryInput struct {
	Name                      string  `json:"name"`
	URL                       string  `json:"url"`
	Branch                    string  `json:"branch"`
	Path                      string  `json:"path"`
	AccessUsername            string  `json:"access_username"`
	AccessToken               *string `json:"access_token"`
	ClearAccessToken          bool    `json:"clear_access_token"`
	PostCommand               string  `json:"post_command"`
	PostCommandTimeoutSeconds int     `json:"post_command_timeout_seconds"`
	HardReset                 bool    `json:"hard_reset"`
	WebhookSecret             *string `json:"webhook_secret"`
}

type Repository struct {
	ID                        string          `json:"id"`
	Name                      string          `json:"name"`
	URL                       string          `json:"url"`
	Branch                    string          `json:"branch"`
	ResolvedBranch            string          `json:"resolved_branch"`
	Path                      string          `json:"path"`
	AccessUsername            string          `json:"access_username"`
	PostCommand               string          `json:"post_command"`
	PostCommandTimeoutSeconds int             `json:"post_command_timeout_seconds"`
	HardReset                 bool            `json:"hard_reset"`
	HasAccessToken            bool            `json:"has_access_token"`
	WebhookSecret             string          `json:"webhook_secret"`
	WebhookURL                string          `json:"webhook_url"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
	LastActivity              *model.Activity `json:"last_activity,omitempty"`
	RunningActivityID         string          `json:"running_activity_id,omitempty"`
}

type Attention struct {
	RepositoryID   string               `json:"repository_id"`
	RepositoryName string               `json:"repository_name"`
	ActivityID     string               `json:"activity_id"`
	Status         model.ActivityStatus `json:"status"`
	FailedStage    model.Stage          `json:"failed_stage"`
	Summary        string               `json:"summary"`
	FinishedAt     time.Time            `json:"finished_at"`
}

type Dashboard struct {
	RepositoryCount int              `json:"repository_count"`
	Running         []model.Activity `json:"running"`
	Recent          []model.Activity `json:"recent"`
	Attention       []Attention      `json:"attention"`
}

type ActivityWithLog struct {
	model.Activity
	Log string `json:"log,omitempty"`
}

func New(cfg config.Config, repos store.Repositories, activities store.Activities, manager Manager, hub *events.Hub, logs logstore.Store, cleaner interface {
	DeleteRepository(context.Context, string) error
}) *Service {
	if hub == nil {
		hub = events.NewHub()
	}
	return &Service{cfg: cfg, repos: repos, activities: activities, manager: manager, hub: hub, logs: logs, cleaner: cleaner}
}

func (s *Service) Meta(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	_ = user
	respond.JSON(w, http.StatusOK, map[string]any{
		"version":                          "dev",
		"base_url":                         strings.TrimRight(s.cfg.Server.BaseURL, "/"),
		"history_max_items":                s.cfg.History.MaxItems,
		"repos_root":                       s.cfg.Storage.ReposRoot,
		"global_webhook_secret_configured": s.cfg.Webhooks.Secret != "",
		"data_dir":                         s.cfg.Storage.DataDir,
	})
}

func (s *Service) Dashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ctx := r.Context()
	repos, err := s.repos.List(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load repositories")
		return
	}
	running, err := s.activities.ListActive(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load running activities")
		return
	}
	recent, err := s.activities.ListRecent(ctx, 20)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load dashboard activity history")
		return
	}
	attention, err := s.activities.LatestFinishedPerRepository(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load attention summary")
		return
	}
	filtered := make([]Attention, 0, len(attention))
	for _, a := range attention {
		if a.Status == model.ActivityFailed || a.Status == model.ActivityInterrupted {
			repo, err := s.repos.Get(ctx, a.RepositoryID)
			if err != nil {
				continue
			}
			filtered = append(filtered, Attention{RepositoryID: a.RepositoryID, RepositoryName: repo.Name, ActivityID: a.ID, Status: a.Status, FailedStage: a.FailedStage, Summary: a.Summary, FinishedAt: a.FinishedAt.UTC()})
		}
	}
	respond.JSON(w, http.StatusOK, Dashboard{RepositoryCount: len(repos), Running: running, Recent: recent, Attention: filtered})
}

func (s *Service) ListRepositories(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ctx := r.Context()
	repos, err := s.repos.List(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not list repositories")
		return
	}
	all, err := s.activities.ListRecent(ctx, 0)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load repository activity history")
		return
	}
	latestByRepo := map[string]model.Activity{}
	for _, act := range all {
		if !act.Status.IsFinished() {
			continue
		}
		if _, exists := latestByRepo[act.RepositoryID]; !exists || act.CreatedAt.After(latestByRepo[act.RepositoryID].CreatedAt) {
			latestByRepo[act.RepositoryID] = act
		}
	}
	for _, act := range all {
		if _, exists := latestByRepo[act.RepositoryID]; !exists {
			latestByRepo[act.RepositoryID] = act
		}
	}
	active, err := s.activities.ListActive(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load active repository activity state")
		return
	}
	activeByRepo := map[string]string{}
	for _, act := range active {
		activeByRepo[act.RepositoryID] = act.ID
	}
	out := make([]Repository, 0, len(repos))
	for _, repo := range repos {
		v := repositoryOut(repo, s.cfg.Server.BaseURL)
		if act, ok := latestByRepo[repo.ID]; ok {
			copy := act
			v.LastActivity = &copy
		}
		if id, ok := activeByRepo[repo.ID]; ok {
			v.RunningActivityID = id
		}
		out = append(out, v)
	}
	respond.JSON(w, http.StatusOK, out)
}

func (s *Service) GetRepository(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	repo, err := s.repos.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
			return
		}
		slog.Error("failed to load repository", "component", "api", "method", r.Method, "path", r.URL.Path, "repository_id", id, "error", err)
		respond.Error(w, http.StatusInternalServerError, "internal", "failed to load repository")
		return
	}
	view := repositoryOut(*repo, s.cfg.Server.BaseURL)
	list, err := s.activities.ListByRepository(ctx, repo.ID, 20, nil)
	if err == nil {
		for _, act := range list {
			if act.Status.IsFinished() {
				copy := act
				view.LastActivity = &copy
				break
			}
		}
		if view.LastActivity == nil && len(list) > 0 {
			copy := list[0]
			view.LastActivity = &copy
		}
	}
	active, err := s.activities.ListActive(ctx)
	if err == nil {
		for _, act := range active {
			if act.RepositoryID == repo.ID {
				view.RunningActivityID = act.ID
				if act.Status == model.ActivityRunning {
					break
				}
			}
		}
	}
	respond.JSON(w, http.StatusOK, view)
}

func decodeRepositoryInputBody(r io.Reader) (RepositoryInput, map[string]bool, error) {
	if r == nil {
		return RepositoryInput{}, map[string]bool{}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return RepositoryInput{}, nil, err
	}
	input := RepositoryInput{}
	present := make(map[string]bool, len(raw))
	for key, value := range raw {
		present[key] = true
		switch key {
		case "name":
			if err := json.Unmarshal(value, &input.Name); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "url":
			if err := json.Unmarshal(value, &input.URL); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "branch":
			if err := json.Unmarshal(value, &input.Branch); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "path":
			if err := json.Unmarshal(value, &input.Path); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "access_username":
			if err := json.Unmarshal(value, &input.AccessUsername); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "access_token":
			var token *string
			if err := json.Unmarshal(value, &token); err != nil {
				return RepositoryInput{}, nil, err
			}
			input.AccessToken = token
		case "clear_access_token":
			if err := json.Unmarshal(value, &input.ClearAccessToken); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "post_command":
			if err := json.Unmarshal(value, &input.PostCommand); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "post_command_timeout_seconds":
			if err := json.Unmarshal(value, &input.PostCommandTimeoutSeconds); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "hard_reset":
			if err := json.Unmarshal(value, &input.HardReset); err != nil {
				return RepositoryInput{}, nil, err
			}
		case "webhook_secret":
			var secret *string
			if err := json.Unmarshal(value, &secret); err != nil {
				return RepositoryInput{}, nil, err
			}
			input.WebhookSecret = secret
		}
	}
	return input, present, nil
}

func repositoryInputFromModel(repo model.Repository) RepositoryInput {
	return RepositoryInput{
		Name:                      repo.Name,
		URL:                       repo.URL,
		Branch:                    repo.Branch,
		Path:                      repo.Path,
		AccessUsername:            repo.AccessUsername,
		PostCommand:               repo.PostCommand,
		PostCommandTimeoutSeconds: repo.PostCommandTimeoutSeconds,
		HardReset:                 repo.HardReset,
	}
}

func applyRepositoryUpdate(existing model.Repository, input RepositoryInput, present map[string]bool) model.Repository {
	updated := existing
	if present["name"] {
		updated.Name = strings.TrimSpace(input.Name)
	}
	if present["url"] {
		updated.URL = input.URL
	}
	if present["branch"] {
		updated.Branch = input.Branch
	}
	if present["path"] {
		updated.Path = input.Path
	}
	if present["access_username"] {
		updated.AccessUsername = input.AccessUsername
	}
	if present["access_token"] {
		if input.AccessToken == nil || *input.AccessToken == "" {
			updated.AccessToken = ""
		} else {
			updated.AccessToken = *input.AccessToken
		}
	}
	if present["clear_access_token"] && input.ClearAccessToken {
		updated.AccessToken = ""
	}
	if present["post_command"] || present["post_command_timeout_seconds"] || present["hard_reset"] {
		updated.PostCommand = input.PostCommand
		updated.PostCommandTimeoutSeconds = input.PostCommandTimeoutSeconds
		updated.HardReset = input.HardReset
	}
	if present["webhook_secret"] {
		if input.WebhookSecret == nil || *input.WebhookSecret == "" {
			updated.WebhookSecret = ""
		} else {
			updated.WebhookSecret = *input.WebhookSecret
		}
	}
	if updated.AccessUsername == "" {
		updated.AccessUsername = "git"
	}
	return updated
}

func (s *Service) createRepositoryForm(r *http.Request) (model.Repository, *RepositoryInput, error) {
	var input RepositoryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		return model.Repository{}, nil, fmt.Errorf("decode repository payload: %w", err)
	}
	if err := validateRepositoryInput(s.cfg, s.repos, r.Context(), input, true, ""); err != nil {
		return model.Repository{}, nil, err
	}
	repo := model.Repository{
		Name:                      strings.TrimSpace(input.Name),
		URL:                       input.URL,
		Branch:                    input.Branch,
		Path:                      input.Path,
		AccessUsername:            input.AccessUsername,
		PostCommand:               input.PostCommand,
		PostCommandTimeoutSeconds: input.PostCommandTimeoutSeconds,
		HardReset:                 input.HardReset,
	}
	if input.AccessToken != nil {
		repo.AccessToken = *input.AccessToken
	}
	if input.WebhookSecret == nil || *input.WebhookSecret == "" {
		secret, err := generateWebhookSecret()
		if err != nil {
			return model.Repository{}, nil, fmt.Errorf("generate webhook secret: %w", err)
		}
		repo.WebhookSecret = secret
	} else {
		repo.WebhookSecret = *input.WebhookSecret
	}
	if repo.AccessUsername == "" {
		repo.AccessUsername = "git"
	}
	return repo, &input, nil
}

func (s *Service) PostRepository(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	repo, _, err := s.createRepositoryForm(r)
	if err != nil {
		var bad *apiError
		if errors.As(err, &bad) {
			respond.Error(w, http.StatusBadRequest, bad.Code, bad.Message)
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.repos.Create(r.Context(), &repo); err != nil {
		if errors.Is(err, store.ErrConflict) {
			respond.Error(w, http.StatusConflict, "conflict", "repository name is already in use")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not create repository")
		return
	}
	s.hub.Publish(events.Event{Type: "repository.created", Data: map[string]any{"id": repo.ID}})
	user, _ := auth.UserFromContext(r.Context())
	triggeredBy := user.Email
	if triggeredBy == "" {
		triggeredBy = user.Sub
	}
	if _, err := s.manager.Submit(r.Context(), repo.ID, syncer.TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual, TriggeredBy: triggeredBy}); err != nil && !errors.Is(err, syncer.ErrBusy) {
		s.loggerWarn("initial sync submission failed", "repository_id", repo.ID, "error", err)
	}
	respond.JSON(w, http.StatusCreated, repositoryOut(repo, s.cfg.Server.BaseURL))
}

func (s *Service) UpdateRepository(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	existing, err := s.repos.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "failed to load repository")
		return
	}
	input, present, err := decodeRepositoryInputBody(r.Body)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "bad_request", "invalid repository payload")
		return
	}
	updated := applyRepositoryUpdate(*existing, input, present)
	if err := validateRepositoryInput(s.cfg, s.repos, ctx, repositoryInputFromModel(updated), false, existing.ID); err != nil {
		var bad *apiError
		if errors.As(err, &bad) {
			respond.Error(w, http.StatusBadRequest, bad.Code, bad.Message)
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if err := s.repos.Update(ctx, &updated); err != nil {
		if errors.Is(err, store.ErrConflict) {
			respond.Error(w, http.StatusConflict, "conflict", "repository name is already in use")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not update repository")
		return
	}
	s.hub.Publish(events.Event{Type: "repository.updated", Data: map[string]any{"id": updated.ID}})
	respond.JSON(w, http.StatusOK, repositoryOut(updated, s.cfg.Server.BaseURL))
}

func (s *Service) DeleteRepository(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if s.manager != nil && s.manager.IsBusy(r.PathValue("id")) {
		respond.Error(w, http.StatusConflict, "conflict", "repository is busy")
		return
	}
	id := r.PathValue("id")
	repo, err := s.repos.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "failed to load repository")
		return
	}
	deleteFiles := r.URL.Query().Get("delete_files") == "true"
	if deleteFiles {
		if err := validateDeletePath(s.cfg.Storage.DataDir, repo.Path); err != nil {
			respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if err := ensureGitDir(repo.Path); err != nil {
			respond.Error(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		if err := os.RemoveAll(repo.Path); err != nil {
			respond.Error(w, http.StatusInternalServerError, "internal", "could not delete repository files")
			return
		}
	}
	if s.cleaner != nil {
		if err := s.cleaner.DeleteRepository(r.Context(), repo.ID); err != nil {
			respond.Error(w, http.StatusInternalServerError, "internal", "could not delete repository history")
			return
		}
	}
	if err := s.repos.Delete(r.Context(), repo.ID); err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not delete repository")
		return
	}
	s.hub.Publish(events.Event{Type: "repository.deleted", Data: map[string]any{"id": repo.ID}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) SyncRepository(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id := r.PathValue("id")
	user, _ := auth.UserFromContext(r.Context())
	triggeredBy := user.Email
	if triggeredBy == "" {
		triggeredBy = user.Sub
	}
	act, err := s.manager.Submit(r.Context(), id, syncer.TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual, TriggeredBy: triggeredBy})
	if errors.Is(err, syncer.ErrBusy) {
		respond.Error(w, http.StatusConflict, "conflict", "repository already queued or running")
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not queue sync")
		return
	}
	respond.JSON(w, http.StatusAccepted, act)
}

func (s *Service) SyncAll(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	triggeredBy := user.Email
	if triggeredBy == "" {
		triggeredBy = user.Sub
	}
	started, skipped, err := s.manager.SubmitAll(r.Context(), syncer.TriggerInfo{Trigger: model.TriggerManual, Source: model.SourceManual, TriggeredBy: triggeredBy})
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not queue syncs")
		return
	}
	respond.JSON(w, http.StatusAccepted, map[string]any{"started": started, "skipped": skipped})
}

func (s *Service) RepositoryActivities(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 {
			respond.Error(w, http.StatusBadRequest, "bad_request", "limit must be a positive integer")
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}
	var before *time.Time
	if q := r.URL.Query().Get("before"); q != "" {
		t, err := time.Parse(time.RFC3339, q)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, "bad_request", "before must be RFC3339")
			return
		}
		before = &t
	}
	acts, err := s.activities.ListByRepository(ctx, id, limit, before)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
			respond.Error(w, http.StatusNotFound, "not_found", "repository not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load repository activities")
		return
	}
	respond.JSON(w, http.StatusOK, acts)
}

func (s *Service) GetActivity(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id := r.PathValue("id")
	act, err := s.activities.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "activity not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load activity")
		return
	}
	logValue := ""
	if s.logs != nil {
		if data, err := s.logs.ReadAll(act.RepositoryID, act.ID); err == nil {
			logValue = trimUTF8Boundaries(string(data))
		} else if errors.Is(err, logstore.ErrNotFound) {
			logValue = "[log file not found]\n"
		} else {
			logValue = "[log read failed]\n"
		}
	}
	respond.JSON(w, http.StatusOK, ActivityWithLog{Activity: *act, Log: logValue})
}

func (s *Service) ActivityLog(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id := r.PathValue("id")
	act, err := s.activities.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respond.Error(w, http.StatusNotFound, "not_found", "activity not found")
			return
		}
		respond.Error(w, http.StatusInternalServerError, "internal", "could not load activity")
		return
	}
	body, err := s.readActivityLog(act)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "internal", "could not read activity log")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if r.URL.Query().Get("download") == "1" {
		repo, err := s.repos.Get(r.Context(), act.RepositoryID)
		if err == nil {
			name := fmt.Sprintf("%s-%s.log", sanitizeFilename(repo.Name), act.ID)
			w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
		}
	}
	_, _ = w.Write([]byte(body))
}

func (s *Service) Events(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ctrl := http.NewResponseController(w)
	_ = ctrl.SetWriteDeadline(time.Time{})
	flusher, _ := w.(http.Flusher)
	_ = writeSSEEvent(w, flusher, "hello", map[string]string{"server_time": time.Now().UTC().Format(time.RFC3339)})
	subCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	ch := s.hub.Subscribe(subCtx)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_ = writeSSEComment(w, flusher, " ping")
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, flusher, event.Type, event.Data); err != nil {
				return
			}
		}
	}
}

func (s *Service) ActivityStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.UserFromContext(r.Context()); !ok {
		respond.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id := r.PathValue("id")
	act, err := s.activities.Get(r.Context(), id)
	if err != nil {
		respond.Error(w, http.StatusNotFound, "not_found", "activity not found")
		return
	}
	var offset int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 0 {
			respond.Error(w, http.StatusBadRequest, "bad_request", "Last-Event-ID must be a non-negative integer offset")
			return
		}
		offset = parsed
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ctrl := http.NewResponseController(w)
	_ = ctrl.SetWriteDeadline(time.Time{})
	flusher, _ := w.(http.Flusher)
	s.writeActivityStream(respondingWriter{ResponseWriter: w, Flusher: flusher}, act, offset, r.Context())
}

type respondingWriter struct {
	http.ResponseWriter
	http.Flusher
}

func (s *Service) writeActivityStream(w respondingWriter, act *model.Activity, offset int64, ctx context.Context) {
	if act.Status.IsFinished() {
		content, err := s.readActivityLog(act)
		if err != nil {
			content = "[log read failed]\n"
		}
		if offset > 0 {
			if offset >= int64(len(content)) {
				_ = writeSSEEventID(w, w.Flusher, "end", strconv.FormatInt(offset, 10), map[string]string{"status": string(act.Status)})
				return
			}
			chunk := content[offset:]
			finalOffset := int64(len(content))
			_ = writeSSEEventID(w, w.Flusher, "log", strconv.FormatInt(finalOffset, 10), map[string]any{"chunk": trimUTF8Boundaries(chunk), "offset": finalOffset})
			_ = writeSSEEventID(w, w.Flusher, "end", strconv.FormatInt(int64(len(content)), 10), map[string]string{"status": string(act.Status)})
			return
		}
		_ = writeSSEEventID(w, w.Flusher, "snapshot", strconv.FormatInt(int64(len(content)), 10), map[string]any{"status": act.Status, "log": trimUTF8Boundaries(content), "offset": int64(len(content))})
		_ = writeSSEEventID(w, w.Flusher, "end", strconv.FormatInt(int64(len(content)), 10), map[string]string{"status": string(act.Status)})
		return
	}
	if offset == 0 {
		data, err := s.readActivityLog(act)
		if err != nil {
			data = "[log read failed]\n"
		}
		_ = writeSSEEventID(w, w.Flusher, "snapshot", "0", map[string]any{"status": act.Status, "log": trimUTF8Boundaries(data), "offset": int64(len(data))})
	}
	live := s.manager.LiveLog(act.ID)
	if live == nil {
		_ = writeSSEEventID(w, w.Flusher, "end", strconv.FormatInt(offset, 10), map[string]string{"status": string(act.Status)})
		return
	}
	for chunk := range live.Subscribe(ctx, offset) {
		_ = writeSSEEventID(w, w.Flusher, "log", strconv.FormatInt(chunk.Offset, 10), map[string]any{"chunk": chunk.Data, "offset": chunk.Offset})
	}
	if act, err := s.activities.Get(ctx, act.ID); err == nil && act.Status.IsFinished() {
		_ = writeSSEEventID(w, w.Flusher, "end", strconv.FormatInt(offset, 10), map[string]string{"status": string(act.Status)})
	}
}

func (s *Service) readActivityLog(act *model.Activity) (string, error) {
	if s.logs == nil {
		return "", nil
	}
	data, err := s.logs.ReadAll(act.RepositoryID, act.ID)
	if errors.Is(err, logstore.ErrNotFound) {
		return "[log file not found]\n", nil
	}
	if err != nil {
		return "", err
	}
	return trimUTF8Boundaries(string(data)), nil
}

func trimUTF8Boundaries(s string) string {
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

type apiError struct{ Code, Message string }

func (e *apiError) Error() string { return e.Message }

func badRequest(msg string) error { return &apiError{Code: "bad_request", Message: msg} }

func validateRepositoryInput(cfg config.Config, repos store.Repositories, ctx context.Context, input RepositoryInput, create bool, currentID string) error {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return badRequest("name is required")
	}
	if len(name) > 100 {
		return badRequest("name must be 1-100 characters")
	}
	if input.URL == "" {
		return badRequest("url is required")
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
		return badRequest("url must be a valid http or https URL without embedded credentials")
	}
	if input.Branch != "" {
		if strings.ContainsAny(input.Branch, " \t\r\n") || strings.Contains(input.Branch, "..") || strings.HasPrefix(input.Branch, "-") {
			return badRequest("branch contains invalid characters")
		}
	}
	if input.Path == "" {
		return badRequest("path is required")
	}
	clean := filepath.Clean(input.Path)
	if clean != input.Path {
		return badRequest("path must be cleaned and absolute")
	}
	if !filepath.IsAbs(clean) || clean == "/" {
		return badRequest("path must be an absolute clone path")
	}
	containsSymlink, err := pathContainsSymlink(clean)
	if err != nil {
		return badRequest("path could not be checked for symlinks")
	}
	if containsSymlink {
		return badRequest("path cannot traverse a symlink")
	}
	if isInsideDir(clean, cfg.Storage.DataDir) {
		return badRequest("path cannot be inside the storage data_dir")
	}
	if repos != nil {
		if create {
			if _, err := repos.GetByName(ctx, name); err == nil {
				return &apiError{Code: "conflict", Message: "repository name is already in use"}
			}
		} else if currentID != "" {
			if existing, err := repos.GetByName(ctx, name); err == nil && existing.ID != currentID {
				return &apiError{Code: "conflict", Message: "repository name is already in use"}
			}
		}
		all, err := repos.List(ctx)
		if err == nil {
			for _, repo := range all {
				if repo.ID == "" || (currentID != "" && repo.ID == currentID) {
					continue
				}
				if isPathOverlap(clean, repo.Path) {
					return badRequest("path overlaps another repository path")
				}
			}
		}
	}
	if input.PostCommandTimeoutSeconds < 0 {
		return badRequest("post_command_timeout_seconds must be >= 0")
	}
	if input.AccessUsername == "" {
		input.AccessUsername = "git"
	}
	return nil
}

func isInsideDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}

func isPathOverlap(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" {
		return "repository"
	}
	return name
}

func validateDeletePath(dataDir, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("path must be an absolute clone path")
	}
	containsSymlink, err := pathContainsSymlink(path)
	if err != nil {
		return fmt.Errorf("check repository path for symlinks: %w", err)
	}
	if containsSymlink {
		return errors.New("path cannot traverse a symlink")
	}
	if isInsideDir(filepath.Clean(path), filepath.Clean(dataDir)) {
		return errors.New("path cannot be inside the data_dir")
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if os.IsNotExist(err) {
			return errors.New("path must contain a .git directory")
		}
		return fmt.Errorf("check repository path: %w", err)
	}
	return nil
}

func pathContainsSymlink(path string) (bool, error) {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	current := volume + string(filepath.Separator)
	remainder := strings.TrimPrefix(clean, current)
	for _, part := range strings.Split(remainder, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func ensureGitDir(path string) error {
	if info, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		if os.IsNotExist(err) {
			return errors.New("path must contain a .git directory")
		}
		return err
	} else if !info.IsDir() {
		return errors.New("path must contain a .git directory")
	}
	return nil
}

func repositoryOut(repo model.Repository, baseURL string) Repository {
	return Repository{ID: repo.ID, Name: repo.Name, URL: safeRepositoryURL(repo.URL), Branch: repo.Branch, ResolvedBranch: repo.ResolvedBranch, Path: repo.Path, AccessUsername: repo.AccessUsername, PostCommand: repo.PostCommand, PostCommandTimeoutSeconds: repo.PostCommandTimeoutSeconds, HardReset: repo.HardReset, HasAccessToken: repo.AccessToken != "", WebhookSecret: repo.WebhookSecret, WebhookURL: strings.TrimRight(baseURL, "/") + "/hooks/" + repo.ID, CreatedAt: repo.CreatedAt, UpdatedAt: repo.UpdatedAt}
}

func safeRepositoryURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return "[invalid repository URL]"
	}
	parsed.User = nil
	return parsed.String()
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, kind string, data any) error {
	return writeSSEEventID(w, flusher, kind, "", data)
}

func writeSSEEventID(w http.ResponseWriter, flusher http.Flusher, kind, id string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, payload); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func writeSSEComment(w http.ResponseWriter, flusher http.Flusher, text string) error {
	if _, err := fmt.Fprintf(w, ": %s\n\n", text); err != nil {
		return err
	}
	if flusher != nil {
		flusher.Flush()
	}
	return nil
}

func (s *Service) loggerWarn(msg string, args ...any) {
	slog.Warn(msg, append([]any{"component", "api"}, args...)...)
}
