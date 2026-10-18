// Package store defines persistence interfaces and shared errors.
package store

import (
	"context"
	"errors"
	"time"

	"gitsync/internal/model"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
	ErrClosed   = errors.New("store: closed")
)

type Repositories interface {
	Create(context.Context, *model.Repository) error
	Get(context.Context, string) (*model.Repository, error)
	GetByName(context.Context, string) (*model.Repository, error)
	List(context.Context) ([]model.Repository, error)
	Update(context.Context, *model.Repository) error
	Delete(context.Context, string) error
}

type Activities interface {
	Create(context.Context, *model.Activity) error
	Get(context.Context, string) (*model.Activity, error)
	Update(context.Context, *model.Activity) error
	ListByRepository(context.Context, string, int, *time.Time) ([]model.Activity, error)
	ListRecent(context.Context, int) ([]model.Activity, error)
	ListActive(context.Context) ([]model.Activity, error)
	LatestFinishedPerRepository(context.Context) ([]model.Activity, error)
	ListFinishedBeyond(context.Context, string, int) ([]model.Activity, error)
	DeleteMany(context.Context, []string) error
	DeleteByRepository(context.Context, string) ([]string, error)
}
