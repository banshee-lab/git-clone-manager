package model

import "time"

// Repository is the persisted configuration and metadata for a Git repository.
type Repository struct {
	ID                        string    `json:"id"`
	Name                      string    `json:"name"`
	URL                       string    `json:"url"`
	Branch                    string    `json:"branch"`
	ResolvedBranch            string    `json:"resolved_branch"`
	Path                      string    `json:"path"`
	AccessUsername            string    `json:"access_username"`
	AccessToken               string    `json:"access_token"`
	PostCommand               string    `json:"post_command"`
	PostCommandTimeoutSeconds int       `json:"post_command_timeout_seconds"`
	HardReset                 bool      `json:"hard_reset"`
	WebhookSecret             string    `json:"webhook_secret"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type ActivityStatus string

const (
	ActivityQueued      ActivityStatus = "queued"
	ActivityRunning     ActivityStatus = "running"
	ActivitySuccess     ActivityStatus = "success"
	ActivityFailed      ActivityStatus = "failed"
	ActivityInterrupted ActivityStatus = "interrupted"
)

func (s ActivityStatus) Valid() bool {
	switch s {
	case ActivityQueued, ActivityRunning, ActivitySuccess, ActivityFailed, ActivityInterrupted:
		return true
	default:
		return false
	}
}

func (s ActivityStatus) IsFinished() bool {
	return s == ActivitySuccess || s == ActivityFailed || s == ActivityInterrupted
}

type Trigger string

const (
	TriggerManual  Trigger = "manual"
	TriggerWebhook Trigger = "webhook"
)

func (v Trigger) Valid() bool { return v == TriggerManual || v == TriggerWebhook }

type Source string

const (
	SourceManual Source = "manual"
	SourceGitHub Source = "github"
	SourceGitLab Source = "gitlab"
	SourceOther  Source = "other"
)

func (v Source) Valid() bool {
	return v == SourceManual || v == SourceGitHub || v == SourceGitLab || v == SourceOther
}

type Action string

const (
	ActionClone Action = "clone"
	ActionPull  Action = "pull"
)

func (v Action) Valid() bool { return v == "" || v == ActionClone || v == ActionPull }

type Stage string

const (
	StageClone       Stage = "clone"
	StageFetch       Stage = "fetch"
	StageCheckout    Stage = "checkout"
	StageMerge       Stage = "merge"
	StageReset       Stage = "reset"
	StagePostCommand Stage = "post_command"
	StageInternal    Stage = "internal"
)

func (v Stage) Valid() bool {
	return v == "" || v == StageClone || v == StageFetch || v == StageCheckout || v == StageMerge || v == StageReset || v == StagePostCommand || v == StageInternal
}

// Activity stores sync metadata only; console output belongs in log files.
type Activity struct {
	ID             string         `json:"id"`
	RepositoryID   string         `json:"repository_id"`
	RepositoryName string         `json:"repository_name"`
	Status         ActivityStatus `json:"status"`
	Trigger        Trigger        `json:"trigger"`
	Source         Source         `json:"source"`
	TriggeredBy    string         `json:"triggered_by"`
	Action         Action         `json:"action"`
	FailedStage    Stage          `json:"failed_stage"`
	Summary        string         `json:"summary"`
	CommitBefore   string         `json:"commit_before"`
	CommitAfter    string         `json:"commit_after"`
	CreatedAt      time.Time      `json:"created_at"`
	StartedAt      *time.Time     `json:"started_at"`
	FinishedAt     *time.Time     `json:"finished_at"`
}

func (a Activity) Valid() bool {
	return a.Status.Valid() && a.Trigger.Valid() && a.Source.Valid() && a.Action.Valid() && a.FailedStage.Valid()
}
