package store_test

import (
	"testing"

	"gitsync/internal/store"
	"gitsync/internal/store/storetest"
)

func TestMemoryStoreContract(t *testing.T) {
	storetest.RunSuite(t, func(*testing.T) (store.Repositories, store.Activities, error) {
		repositories, activities := store.NewMemory()
		return repositories, activities, nil
	})
}
