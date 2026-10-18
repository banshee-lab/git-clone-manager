package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestActivityEnumsAndMetadataOnly(t *testing.T) {
	for _, status := range []ActivityStatus{ActivityQueued, ActivityRunning, ActivitySuccess, ActivityFailed, ActivityInterrupted} {
		if !status.Valid() {
			t.Errorf("%q should be valid", status)
		}
		if status.IsFinished() != (status == ActivitySuccess || status == ActivityFailed || status == ActivityInterrupted) {
			t.Errorf("IsFinished incorrect for %q", status)
		}
	}
	if (ActivityStatus("unknown")).Valid() || (Trigger("unknown")).Valid() || (Source("unknown")).Valid() || (Action("unknown")).Valid() || (Stage("unknown")).Valid() {
		t.Fatal("unknown enum value was accepted")
	}
	activity := Activity{ID: "id", RepositoryID: "repo", Status: ActivitySuccess, Trigger: TriggerManual, Source: SourceManual, CreatedAt: time.Now().UTC()}
	data, err := json.Marshal(activity)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "log") {
		t.Fatalf("activity JSON must not contain console output: %s", data)
	}
}
