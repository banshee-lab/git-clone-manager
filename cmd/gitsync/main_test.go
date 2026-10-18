package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitsync/internal/config"
	"gitsync/internal/events"
	"gitsync/internal/model"
	"gitsync/internal/store"
	"gitsync/internal/syncer"
)

func TestHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	healthz(recorder, httptest.NewRequest("GET", "/healthz", nil))

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got, want := recorder.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

type webhookRouteManager struct {
	submitted int
}

func (m *webhookRouteManager) Submit(_ context.Context, repoID string, info syncer.TriggerInfo) (model.Activity, error) {
	m.submitted++
	return model.Activity{ID: "activity-1", RepositoryID: repoID, Trigger: info.Trigger, Source: info.Source}, nil
}

func (m *webhookRouteManager) SubmitAll(context.Context, syncer.TriggerInfo) ([]model.Activity, []syncer.Skip, error) {
	return nil, nil, nil
}

func (*webhookRouteManager) IsBusy(string) bool             { return false }
func (*webhookRouteManager) LiveLog(string) *syncer.LiveLog { return nil }

func TestWebhookRouteNeedsNoSessionAndBypassesCSRF(t *testing.T) {
	const repositoryID = "7e3a5d18-17f7-4b9f-a44d-8898bd2d2d97"
	repositories, activities := store.NewMemory()
	repo := model.Repository{ID: repositoryID, Name: "demo", WebhookSecret: "secret"}
	if err := repositories.Create(context.Background(), &repo); err != nil {
		t.Fatal(err)
	}
	manager := &webhookRouteManager{}
	cfg := config.Default()
	cfg.Server.BaseURL = "https://app.example.test"
	handler, err := newMux(cfg, nil, repositories, activities, manager, events.NewHub(), nil, nil)
	if err != nil {
		t.Fatalf("create mux: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/hooks/"+repositoryID, http.NoBody)
	request.Header.Set("X-Token", "secret")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || manager.submitted != 1 {
		t.Fatalf("status=%d submits=%d body=%s", response.Code, manager.submitted, response.Body.String())
	}
}
