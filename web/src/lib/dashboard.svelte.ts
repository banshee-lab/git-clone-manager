import { getDashboard, syncRepository, ApiError } from "./api/client";
import { events } from "./events.svelte";
import type { ActivitySummary, Attention, Dashboard } from "./api/types";

export type DashboardStore = ReturnType<typeof createDashboardStore>;

export function createDashboardStore() {
  let repositoryCount = $state(0);
  let running = $state<ActivitySummary[]>([]);
  let recent = $state<ActivitySummary[]>([]);
  let attention = $state<Attention[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let lastSyncTime = $state<string | null>(null);

  /** Replace the full dashboard state from an API response. */
  function applySnapshot(data: Dashboard): void {
    repositoryCount = data.repository_count;
    running = data.running;
    recent = data.recent;
    attention = data.attention;
    const finished = data.recent.filter((activity) => activity.finished_at);
    const latestFinished = finished.sort((a, b) =>
      (b.finished_at ?? "").localeCompare(a.finished_at ?? ""),
    )[0];
    lastSyncTime = latestFinished?.finished_at ?? null;
    error = null;
  }

  /** Full refresh from the API. */
  async function refresh(options: { silent?: boolean } = {}): Promise<void> {
    if (!options.silent) loading = true;
    try {
      const data = await getDashboard();
      applySnapshot(data);
    } catch (caught) {
      error =
        caught instanceof Error ? caught.message : "Unable to load dashboard.";
    } finally {
      if (!options.silent) loading = false;
    }
  }

  /** Handle activity.queued / activity.started / activity.finished events. */
  function handleActivityEvent(payload: unknown): void {
    const act =
      payload && typeof payload === "object" && "activity" in payload
        ? (payload as { activity?: ActivitySummary }).activity
        : undefined;
    if (!act) {
      void refresh({ silent: true });
      return;
    }
    if (
      !["queued", "running", "success", "failed", "interrupted"].includes(
        act.status,
      )
    ) {
      void refresh({ silent: true });
      return;
    }

    // Update running panel
    if (act.status === "queued" || act.status === "running") {
      const idx = running.findIndex((r) => r.id === act.id);
      if (idx >= 0) {
        running[idx] = act;
      } else {
        running = [act, ...running];
      }
    } else {
      // Finished — remove from running
      running = running.filter((r) => r.id !== act.id);
    }

    // Update recent panel: insert or update, keep newest 20
    recent = [act, ...recent.filter((item) => item.id !== act.id)]
      .sort((a, b) => b.created_at.localeCompare(a.created_at))
      .slice(0, 20);

    // Update lastSyncTime for finished activities
    if (
      act.status !== "queued" &&
      act.status !== "running" &&
      act.finished_at
    ) {
      const t = act.finished_at;
      if (!lastSyncTime || t > lastSyncTime) {
        lastSyncTime = t;
      }
    }

    // Update attention panel
    if (act.status === "failed" || act.status === "interrupted") {
      // Add or update attention entry for this repo
      const existingAttIdx = attention.findIndex(
        (a) => a.repository_id === act.repository_id,
      );
      const newItem: Attention = {
        repository_id: act.repository_id,
        repository_name: act.repository_name,
        activity_id: act.id,
        status: act.status,
        failed_stage: act.failed_stage,
        summary: act.summary,
        finished_at: act.finished_at ?? act.created_at,
      };
      if (existingAttIdx >= 0) {
        attention[existingAttIdx] = newItem;
      } else {
        attention = [...attention, newItem];
      }
    } else if (act.status === "success") {
      // A successful sync clears the repo from the attention panel
      attention = attention.filter(
        (a) => a.repository_id !== act.repository_id,
      );
    }
  }

  /** Handle repository.created / repository.updated / repository.deleted events. */
  function handleRepositoryEvent(): void {
    // Trigger a silent full refresh to keep repository count accurate
    void refresh({ silent: true });
  }

  // Wire up SSE subscriptions
  events.subscribe("resync", () => void refresh({ silent: true }));
  events.subscribe("activity.queued", handleActivityEvent);
  events.subscribe("activity.started", handleActivityEvent);
  events.subscribe("activity.finished", handleActivityEvent);
  events.subscribe("repository.created", handleRepositoryEvent);
  events.subscribe("repository.updated", handleRepositoryEvent);
  events.subscribe("repository.deleted", handleRepositoryEvent);

  /** Trigger a sync for a repository and return the new activity, or throw. */
  async function retrySync(repositoryId: string): Promise<ActivitySummary> {
    return syncRepository(repositoryId);
  }

  return {
    get repositoryCount(): number {
      return repositoryCount;
    },
    get running(): ActivitySummary[] {
      return running;
    },
    get recent(): ActivitySummary[] {
      return recent;
    },
    get attention(): Attention[] {
      return attention;
    },
    get loading(): boolean {
      return loading;
    },
    get error(): string | null {
      return error;
    },
    get lastSyncTime(): string | null {
      return lastSyncTime;
    },
    refresh,
    retrySync,
    /** Exposed for testing only */
    handleActivityEvent,
    handleRepositoryEvent,
    ApiError,
  };
}

export const dashboard = createDashboardStore();
