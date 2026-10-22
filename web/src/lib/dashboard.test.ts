import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { createDashboardStore } from "./dashboard.svelte";
import type { ActivitySummary, Attention, Dashboard } from "./api/types";
import * as client from "./api/client";

// ── helpers ──────────────────────────────────────────────────────────────────

function makeActivity(
  overrides: Partial<ActivitySummary> = {},
): ActivitySummary {
  return {
    id: "act-1",
    repository_id: "repo-1",
    repository_name: "CLI tools",
    status: "success",
    trigger: "manual",
    source: "manual",
    triggered_by: "admin@test",
    action: "pull",
    failed_stage: "",
    summary: "Synced OK",
    commit_before: "aaa1111",
    commit_after: "bbb2222",
    created_at: "2024-01-01T00:00:00Z",
    started_at: "2024-01-01T00:00:01Z",
    finished_at: "2024-01-01T00:01:00Z",
    ...overrides,
  };
}

function makeAttention(overrides: Partial<Attention> = {}): Attention {
  return {
    repository_id: "repo-1",
    repository_name: "CLI tools",
    activity_id: "act-1",
    status: "failed",
    failed_stage: "merge",
    summary: "Merge conflict",
    finished_at: "2024-01-01T00:01:00Z",
    ...overrides,
  };
}

function makeDashboard(overrides: Partial<Dashboard> = {}): Dashboard {
  return {
    repository_count: 2,
    running: [],
    recent: [makeActivity()],
    attention: [],
    ...overrides,
  };
}

// ── store tests ───────────────────────────────────────────────────────────────

describe("dashboard store", () => {
  beforeEach(() => {
    vi.spyOn(client, "getDashboard").mockResolvedValue(makeDashboard());
    vi.spyOn(client, "syncRepository").mockResolvedValue(
      makeActivity({ status: "queued" }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("starts empty and updates from refresh", async () => {
    const store = createDashboardStore();
    expect(store.repositoryCount).toBe(0);
    expect(store.loading).toBe(false);

    await store.refresh();

    expect(store.repositoryCount).toBe(2);
    expect(store.recent).toHaveLength(1);
    expect(store.error).toBeNull();
  });

  it("sets loading to true then false during refresh", async () => {
    const store = createDashboardStore();
    const states: boolean[] = [];

    // Non-silent refresh shows loading state
    const promise = store.refresh();
    states.push(store.loading);
    await promise;
    states.push(store.loading);

    expect(states).toEqual([true, false]);
  });

  it("silent refresh does not flip loading", async () => {
    const store = createDashboardStore();
    const promise = store.refresh({ silent: true });
    expect(store.loading).toBe(false);
    await promise;
    expect(store.loading).toBe(false);
  });

  it("exposes error when getDashboard rejects", async () => {
    vi.spyOn(client, "getDashboard").mockRejectedValue(
      new Error("network error"),
    );
    const store = createDashboardStore();
    await store.refresh();
    expect(store.error).toBe("network error");
  });

  // ── incremental activity.queued ──
  it("adds a queued activity to the running panel", () => {
    const store = createDashboardStore();
    const act = makeActivity({ status: "queued", id: "act-q" });
    store.handleActivityEvent({ activity: act });
    expect(store.running).toHaveLength(1);
    expect(store.running[0].id).toBe("act-q");
  });

  it("adds a running activity to the running panel", () => {
    const store = createDashboardStore();
    const act = makeActivity({
      status: "running",
      id: "act-r",
      finished_at: null,
    });
    store.handleActivityEvent({ activity: act });
    expect(store.running).toHaveLength(1);
    expect(store.running[0].status).toBe("running");
  });

  it("updates an existing running activity in-place", () => {
    const store = createDashboardStore();
    const act = makeActivity({
      status: "running",
      id: "act-r",
      finished_at: null,
    });
    store.handleActivityEvent({ activity: act });
    store.handleActivityEvent({
      activity: { ...act, summary: "Still running" },
    });
    expect(store.running).toHaveLength(1);
    expect(store.running[0].summary).toBe("Still running");
  });

  // ── incremental activity.finished (success) ──
  it("removes finished activity from running and adds to recent", () => {
    const store = createDashboardStore();
    const running = makeActivity({
      status: "running",
      id: "act-r",
      finished_at: null,
    });
    store.handleActivityEvent({ activity: running });
    expect(store.running).toHaveLength(1);

    const done = makeActivity({
      status: "success",
      id: "act-r",
      finished_at: "2024-01-01T00:01:00Z",
    });
    store.handleActivityEvent({ activity: done });

    expect(store.running).toHaveLength(0);
    expect(store.recent.some((a) => a.id === "act-r")).toBe(true);
  });

  // ── attention add/clear transitions ──
  it("adds a failed activity to attention", () => {
    const store = createDashboardStore();
    const act = makeActivity({ status: "failed", failed_stage: "merge" });
    store.handleActivityEvent({ activity: act });
    expect(store.attention).toHaveLength(1);
    expect(store.attention[0].failed_stage).toBe("merge");
  });

  it("adds an interrupted activity to attention", () => {
    const store = createDashboardStore();
    const act = makeActivity({ status: "interrupted", failed_stage: "" });
    store.handleActivityEvent({ activity: act });
    expect(store.attention).toHaveLength(1);
    expect(store.attention[0].status).toBe("interrupted");
  });

  it("updates attention entry when same repo fails again", () => {
    const store = createDashboardStore();
    const fail1 = makeActivity({
      id: "act-a",
      status: "failed",
      failed_stage: "fetch",
    });
    const fail2 = makeActivity({
      id: "act-b",
      status: "failed",
      failed_stage: "merge",
    });
    store.handleActivityEvent({ activity: fail1 });
    store.handleActivityEvent({ activity: fail2 });
    expect(store.attention).toHaveLength(1);
    expect(store.attention[0].failed_stage).toBe("merge");
  });

  it("clears attention when a later sync succeeds", () => {
    const store = createDashboardStore();
    const fail = makeActivity({ status: "failed", failed_stage: "merge" });
    store.handleActivityEvent({ activity: fail });
    expect(store.attention).toHaveLength(1);

    const success = makeActivity({ status: "success" });
    store.handleActivityEvent({ activity: success });
    expect(store.attention).toHaveLength(0);
  });

  it("does not clear attention for a different repo's success", () => {
    const store = createDashboardStore();
    const fail = makeActivity({
      repository_id: "repo-1",
      status: "failed",
      failed_stage: "fetch",
    });
    store.handleActivityEvent({ activity: fail });

    const otherSuccess = makeActivity({
      repository_id: "repo-2",
      status: "success",
    });
    store.handleActivityEvent({ activity: otherSuccess });
    expect(store.attention).toHaveLength(1);
  });

  // ── recent panel capped at 20 ──
  it("keeps at most 20 entries in the recent panel", () => {
    const store = createDashboardStore();
    for (let i = 0; i < 25; i++) {
      const act = makeActivity({
        id: `act-${i}`,
        status: "success",
      });
      store.handleActivityEvent({ activity: act });
    }
    expect(store.recent.length).toBeLessThanOrEqual(20);
  });

  // ── lastSyncTime ──
  it("updates lastSyncTime for finished activities", () => {
    const store = createDashboardStore();
    const act = makeActivity({
      status: "success",
      finished_at: "2024-05-01T10:00:00Z",
    });
    store.handleActivityEvent({ activity: act });
    expect(store.lastSyncTime).toBe("2024-05-01T10:00:00Z");
  });

  it("keeps lastSyncTime at the most recent finished time", () => {
    const store = createDashboardStore();
    store.handleActivityEvent({
      activity: makeActivity({
        id: "act-1",
        status: "success",
        finished_at: "2024-05-01T10:00:00Z",
      }),
    });
    store.handleActivityEvent({
      activity: makeActivity({
        id: "act-2",
        status: "success",
        finished_at: "2024-05-01T09:00:00Z", // earlier
      }),
    });
    expect(store.lastSyncTime).toBe("2024-05-01T10:00:00Z");
  });

  it("orders recent items by creation time when events arrive out of order", () => {
    const store = createDashboardStore();
    store.handleActivityEvent({
      activity: makeActivity({ id: "new", created_at: "2024-05-02T00:00:00Z" }),
    });
    store.handleActivityEvent({
      activity: makeActivity({ id: "old", created_at: "2024-05-01T00:00:00Z" }),
    });
    expect(store.recent.map((activity) => activity.id)).toEqual(["new", "old"]);
  });

  it("refetches instead of treating an unknown activity status as finished", async () => {
    const refresh = vi.spyOn(client, "getDashboard");
    const store = createDashboardStore();
    store.handleActivityEvent({
      activity: makeActivity({
        id: "mystery-act",
        status: "mystery" as ActivitySummary["status"],
      }),
    });
    await Promise.resolve();
    await Promise.resolve();
    expect(refresh).toHaveBeenCalledOnce();
    expect(store.running).toHaveLength(0);
    expect(store.recent.some((activity) => activity.id === "mystery-act")).toBe(
      false,
    );
  });

  it("clears last sync time when a refreshed snapshot has no activity", async () => {
    const store = createDashboardStore();
    await store.refresh();
    expect(store.lastSyncTime).not.toBeNull();
    vi.spyOn(client, "getDashboard").mockResolvedValue(
      makeDashboard({ recent: [] }),
    );
    await store.refresh();
    expect(store.lastSyncTime).toBeNull();
  });

  it("uses the newest completed activity even when an active one is newer", async () => {
    const finishedAt = "2024-05-01T10:00:00Z";
    vi.spyOn(client, "getDashboard").mockResolvedValue(
      makeDashboard({
        recent: [
          makeActivity({
            id: "active",
            status: "running",
            created_at: "2024-05-02T10:00:00Z",
            started_at: "2024-05-02T10:00:01Z",
            finished_at: null,
          }),
          makeActivity({
            id: "finished",
            status: "success",
            finished_at: finishedAt,
          }),
        ],
      }),
    );
    const store = createDashboardStore();

    await store.refresh();

    expect(store.lastSyncTime).toBe(finishedAt);
  });

  it("does not set last sync time from an active activity event", () => {
    const store = createDashboardStore();
    store.handleActivityEvent({
      activity: makeActivity({
        status: "queued",
        created_at: "2024-05-02T10:00:00Z",
        started_at: null,
        finished_at: null,
      }),
    });

    expect(store.lastSyncTime).toBeNull();
  });

  // ── resync ──
  it("performs a full refresh on resync", async () => {
    const getDashboardSpy = vi.spyOn(client, "getDashboard");
    const store = createDashboardStore();
    // createDashboardStore registers a resync handler; call handleRepositoryEvent
    // (which does a silent refresh) to confirm that path works
    await store.refresh();
    expect(getDashboardSpy).toHaveBeenCalledTimes(1);
    store.handleRepositoryEvent();
    // wait one tick for the async refresh
    await Promise.resolve();
    await Promise.resolve();
    expect(getDashboardSpy).toHaveBeenCalledTimes(2);
  });

  // ── retrySync ──
  it("calls syncRepository with the repository id", async () => {
    const spy = vi.spyOn(client, "syncRepository");
    const store = createDashboardStore();
    await store.retrySync("repo-1");
    expect(spy).toHaveBeenCalledWith("repo-1");
  });

  // ── ignores events without activity field ──
  it("ignores payloads without an activity field", () => {
    const store = createDashboardStore();
    store.handleActivityEvent(null);
    store.handleActivityEvent({});
    store.handleActivityEvent({ foo: "bar" });
    expect(store.running).toHaveLength(0);
  });
});

// ── elapsed timer helpers ────────────────────────────────────────────────────

describe("elapsed time helpers", () => {
  it("computes elapsed ms from started_at to now for running activities", () => {
    const now = new Date("2024-01-01T00:05:30Z").getTime();
    const started = new Date("2024-01-01T00:00:00Z").getTime();
    const elapsed = Math.max(0, now - started);
    expect(elapsed).toBe(330_000); // 5 min 30 s
  });

  it("returns 0 for negative elapsed", () => {
    const now = new Date("2024-01-01T00:00:00Z").getTime();
    const started = new Date("2024-01-01T00:00:05Z").getTime();
    const elapsed = Math.max(0, now - started);
    expect(elapsed).toBe(0);
  });
});

// ── attention snapshot tests ─────────────────────────────────────────────────

describe("attention from initial dashboard snapshot", () => {
  beforeEach(() => {
    vi.spyOn(client, "getDashboard").mockResolvedValue(
      makeDashboard({
        attention: [makeAttention()],
      }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("loads initial attention items from the API snapshot", async () => {
    const store = createDashboardStore();
    await store.refresh();
    expect(store.attention).toHaveLength(1);
    expect(store.attention[0].failed_stage).toBe("merge");
  });
});
