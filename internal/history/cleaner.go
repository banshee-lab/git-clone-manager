// Package history removes old activity records and their associated log files.
package history

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gitsync/internal/logstore"
	"gitsync/internal/store"
)

type Cleaner struct {
	activities   store.Activities
	repositories store.Repositories
	logs         logstore.Store
	maxItems     int
	logger       *slog.Logger
}

func New(activities store.Activities, repositories store.Repositories, logs logstore.Store, maxItems int) *Cleaner {
	return &Cleaner{activities: activities, repositories: repositories, logs: logs, maxItems: maxItems, logger: slog.Default()}
}

func (c *Cleaner) PruneRepository(ctx context.Context, repoID string) (int, error) {
	if c == nil || c.activities == nil || c.logs == nil {
		return 0, errors.New("history: cleaner is not configured")
	}
	activities, err := c.activities.ListFinishedBeyond(ctx, repoID, c.maxItems)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(activities))
	for _, activity := range activities {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if err := c.logs.Remove(repoID, activity.ID); err != nil {
			c.logger.Warn("could not remove old activity log; keeping record for retry", "repository_id", repoID, "activity_id", activity.ID, "error", err)
			continue
		}
		ids = append(ids, activity.ID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if err := c.activities.DeleteMany(ctx, ids); err != nil {
		return 0, err
	}
	return len(ids), nil
}

func (c *Cleaner) DeleteRepository(ctx context.Context, repoID string) error {
	if c == nil || c.activities == nil || c.logs == nil {
		return errors.New("history: cleaner is not configured")
	}
	if _, err := c.activities.DeleteByRepository(ctx, repoID); err != nil {
		return err
	}
	return c.logs.RemoveRepo(repoID)
}

func (c *Cleaner) Startup(ctx context.Context) error {
	if c == nil || c.activities == nil || c.repositories == nil || c.logs == nil {
		return errors.New("history: cleaner is not configured")
	}
	repos, err := c.repositories.List(ctx)
	if err != nil {
		return err
	}
	total := 0
	known := make(map[string]string)
	for _, repo := range repos {
		deleted, err := c.PruneRepository(ctx, repo.ID)
		if err != nil {
			return err
		}
		total += deleted
		activities, err := c.activities.ListByRepository(ctx, repo.ID, 0, nil)
		if err != nil {
			return fmt.Errorf("history: list activities for sweep: %w", err)
		}
		for _, activity := range activities {
			known[activity.ID] = activity.RepositoryID
		}
	}
	orphans, err := c.logs.Sweep(ctx, func(repoID, actID string) bool { return known[actID] == repoID })
	if err != nil {
		return err
	}
	c.logger.Info("activity history cleanup complete", "pruned_activities", total, "orphan_logs_removed", orphans)
	return nil
}
