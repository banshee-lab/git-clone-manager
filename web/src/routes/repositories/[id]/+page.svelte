<script lang="ts">
  import { onMount } from "svelte";
  import {
    AlertDialog,
    Button,
    CodeBlock,
    Heading,
    Separator,
    StatusDot,
    toast,
  } from "@runwisp/ui";
  import { page } from "$app/state";
  import {
    ApiError,
    deleteRepository,
    getActivity,
    getActivityLog,
    getRepository,
    listActivities,
    syncRepository,
  } from "#lib/api/client";
  import { events } from "#lib/events.svelte";
  import { createLogStream } from "#lib/log-stream.svelte";
  import { repositoryStatusTone } from "#lib/repositories.svelte";
  import { formatDateTime, formatDuration, stripAnsi } from "#lib/utils";
  import type { ActivitySummary, Repository } from "#lib/api/types";

  const ACTIVITY_LIMIT = 20;

  let repository = $state<Repository | null>(null);
  let activities = $state<ActivitySummary[]>([]);
  let activityCursor = $state<string | null>(null);
  let hasMore = $state(false);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let deleteOpen = $state(false);
  let deleteFiles = $state(false);
  let deleteError = $state<string | null>(null);
  let selectedActivityId = $state<string | null>(null);
  let activityLog = $state("");
  let activityLoading = $state(false);
  let autoScroll = $state(true);
  let liveStream = $state<ReturnType<typeof createLogStream> | null>(null);
  let logViewport = $state<HTMLDivElement | null>(null);
  let now = $state(Date.now());

  onMount(() => {
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    const unsubs = [
      events.subscribe("activity.queued", handleActivityEvent),
      events.subscribe("activity.started", handleActivityEvent),
      events.subscribe("activity.finished", handleActivityEvent),
      events.subscribe("repository.updated", () => void loadRepository()),
    ];
    return () => {
      clearInterval(timer);
      unsubs.forEach((unsub) => unsub());
    };
  });

  function updateActivityQuery(id: string | null): void {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (id) {
      url.searchParams.set("activity", id);
    } else {
      url.searchParams.delete("activity");
    }
    window.history.replaceState({}, "", `${url.pathname}${url.search}`);
  }

  async function loadActivityLog(activityId: string): Promise<void> {
    activityLoading = true;
    try {
      const log = await getActivityLog(activityId, false);
      activityLog = stripAnsi(log);
    } catch (error) {
      activityLog =
        error instanceof Error ? error.message : "Unable to load activity log.";
    } finally {
      activityLoading = false;
    }
  }

  function formatActivityDuration(activity: ActivitySummary): string {
    const started = activity.started_at
      ? new Date(activity.started_at).getTime()
      : activity.created_at
        ? new Date(activity.created_at).getTime()
        : null;
    if (!started) return "—";
    const finished = activity.finished_at
      ? new Date(activity.finished_at).getTime()
      : now;
    return formatDuration(Math.max(0, finished - started));
  }

  async function openActivity(activityId: string | null): Promise<void> {
    if (!activityId) {
      selectedActivityId = null;
      updateActivityQuery(null);
      if (liveStream) {
        liveStream.close();
        liveStream = null;
      }
      activityLog = "";
      return;
    }

    selectedActivityId = activityId;
    updateActivityQuery(activityId);

    let picked = activities.find((entry) => entry.id === activityId);
    if (!picked) {
      try {
        const detail = await getActivity(activityId);
        if (!activities.some((entry) => entry.id === detail.id)) {
          activities = [detail, ...activities];
        }
        picked = detail;
      } catch (error) {
        activityLog =
          error instanceof Error ? error.message : "Unable to load activity.";
        return;
      }
    }

    if (picked.status === "running" || picked.status === "queued") {
      if (liveStream) liveStream.close();
      const stream = createLogStream(picked.id);
      liveStream = stream;
      stream.subscribe(() => {
        activityLog = stripAnsi(stream.log || "");
        if (stream.activityStatus) {
          const act = activities.find((entry) => entry.id === picked?.id);
          if (act && act.status !== stream.activityStatus) {
            act.status = stream.activityStatus;
            void loadRepository();
          }
        }
      });
      activityLog = stripAnsi(stream.log || "");
      return;
    }

    if (liveStream) {
      liveStream.close();
      liveStream = null;
    }
    await loadActivityLog(picked.id);
  }

  async function loadActivities(reset = true): Promise<void> {
    const id = page.params.id;
    if (!id) return;
    if (reset) {
      activityCursor = null;
    }
    activityLoading = true;
    try {
      const list = await listActivities(id, {
        limit: ACTIVITY_LIMIT,
        before: reset ? undefined : (activityCursor ?? undefined),
      });
      if (reset) {
        activities = list;
      } else {
        activities = [...activities, ...list];
      }
      hasMore = list.length >= ACTIVITY_LIMIT;
      if (list.length > 0) {
        activityCursor = list[list.length - 1].created_at;
      }

      const currentQuery = page.url.searchParams.get("activity");
      if (currentQuery) {
        void openActivity(currentQuery);
      }
    } catch (error) {
      loadError =
        error instanceof Error
          ? error.message
          : "Unable to load activity history.";
    } finally {
      activityLoading = false;
    }
  }

  async function loadRepository(): Promise<void> {
    const id = page.params.id;
    if (!id) return;
    loading = true;
    loadError = null;
    try {
      const repo = await getRepository(id);
      repository = repo;
      if (!selectedActivityId) {
        const fromQuery = page.url.searchParams.get("activity");
        if (fromQuery) {
          selectedActivityId = fromQuery;
        }
      }
      await loadActivities(true);
    } catch (error) {
      loadError =
        error instanceof Error ? error.message : "Unable to load repository.";
    } finally {
      loading = false;
    }
  }

  function handleActivityEvent(event: unknown): void {
    const payload = (event as { activity?: ActivitySummary })?.activity;
    if (!payload || !repository || payload.repository_id !== repository.id)
      return;
    const existingIndex = activities.findIndex((a) => a.id === payload.id);
    if (existingIndex >= 0) {
      activities[existingIndex] = { ...activities[existingIndex], ...payload };
    } else {
      activities = [payload, ...activities];
    }
    void loadRepository();
  }

  $effect(() => {
    const id = page.params.id;
    if (!id) return;
    void loadRepository();
  });

  $effect(() => {
    const queryId = page.url.searchParams.get("activity");
    if (queryId && queryId !== selectedActivityId) {
      void openActivity(queryId);
    } else if (!queryId && selectedActivityId) {
      void openActivity(null);
    }
  });

  $effect(() => {
    if (!autoScroll || !logViewport) return;
    void activityLog;
    queueMicrotask(() => {
      if (logViewport) {
        logViewport.scrollTop = logViewport.scrollHeight;
      }
    });
  });

  async function handleSyncNow(): Promise<void> {
    if (!repository) return;
    try {
      await syncRepository(repository.id);
      await loadRepository();
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        toast.error(
          error.message || "Repository is busy and cannot be synced right now.",
        );
        return;
      }
      toast.error(
        error instanceof Error ? error.message : "Unable to start sync.",
      );
    }
  }

  async function handleDelete(): Promise<void> {
    if (!repository) return;
    deleteError = null;
    try {
      await deleteRepository(repository.id, deleteFiles);
      window.location.assign("/repositories");
    } catch (error) {
      deleteError =
        error instanceof Error ? error.message : "Unable to delete repository.";
    }
  }

  async function handleCopyLog(): Promise<void> {
    if (!activityLog) return;
    await navigator.clipboard.writeText(activityLog);
    toast.success("Activity log copied.");
  }

  function handleDownloadLog(): void {
    const id = selectedActivityId ?? repository?.id;
    if (!id) return;
    const link = document.createElement("a");
    link.href = `/api/activities/${encodeURIComponent(id)}/log?download=1`;
    link.download = `${repository?.name ?? "activity"}.log`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
  }

  onMount(() => {
    const cleanup = [
      events.subscribe("activity.queued", handleActivityEvent),
      events.subscribe("activity.started", handleActivityEvent),
      events.subscribe("activity.finished", handleActivityEvent),
      events.subscribe("repository.updated", () => void loadRepository()),
    ];
    return () => {
      cleanup.forEach((fn) => fn());
      if (liveStream) liveStream.close();
    };
  });
</script>

<svelte:head>
  <title>{repository?.name ?? "Repository"} · GitSync</title>
</svelte:head>

<section class="page-shell">
  {#if loading}
    <p class="empty-state">Loading repository details…</p>
  {:else if loadError}
    <p class="empty-state error">{loadError}</p>
  {:else if repository}
    <header class="repo-header">
      <div>
        <div class="status-row">
          <StatusDot tone={repositoryStatusTone(repository)} />
          <span>{repository.last_activity?.status ?? "never"}</span>
        </div>
        <Heading level={1} size="lg">{repository.name}</Heading>
      </div>
      <div class="header-actions">
        <Button
          variant="secondary"
          onclick={() => {
            if (repository)
              window.location.href = "/repositories/" + repository.id + "/edit";
          }}
        >
          Edit
        </Button>
        <Button variant="danger" onclick={() => (deleteOpen = true)}
          >Delete</Button
        >
        <Button
          onclick={() => void handleSyncNow()}
          disabled={Boolean(repository?.running_activity_id)}
        >
          Sync now
        </Button>
      </div>
    </header>

    <div class="meta-grid">
      <div class="info-panel">
        <h2>Details</h2>
        <dl>
          <div>
            <dt>URL</dt>
            <dd>{repository.url}</dd>
          </div>
          <div>
            <dt>Branch</dt>
            <dd>{repository.resolved_branch || repository.branch}</dd>
          </div>
          <div>
            <dt>Path</dt>
            <dd>{repository.path}</dd>
          </div>
          <div>
            <dt>Post command</dt>
            <dd>{repository.post_command || "—"}</dd>
          </div>
          <div>
            <dt>Timeout</dt>
            <dd>{repository.post_command_timeout_seconds || "default"}</dd>
          </div>
          <div>
            <dt>Hard reset</dt>
            <dd>{repository.hard_reset ? "On" : "Off"}</dd>
          </div>
          <div>
            <dt>Token</dt>
            <dd>{repository.has_access_token ? "Configured" : "Not set"}</dd>
          </div>
          <div>
            <dt>Created</dt>
            <dd>{formatDateTime(repository.created_at)}</dd>
          </div>
          <div>
            <dt>Updated</dt>
            <dd>{formatDateTime(repository.updated_at)}</dd>
          </div>
        </dl>
      </div>
      <div class="info-panel">
        <h2>Webhook</h2>
        <p class="mono">{repository.webhook_url}</p>
        <p class="muted">
          Secret: {repository.webhook_secret ? "Set" : "Use global"}
        </p>
        {#if repository.webhook_secret}
          <div class="secret-actions-row">
            <Button
              type="button"
              variant="secondary"
              onclick={() => {
                if (repository) {
                  void navigator.clipboard.writeText(
                    repository.webhook_secret ?? "",
                  );
                }
              }}>Copy secret</Button
            >
          </div>
        {/if}
      </div>
    </div>

    <Separator />

    <div class="activity-layout">
      <div class="activities-panel">
        <div class="panel-header">
          <h2>Activity</h2>
          {#if hasMore}
            <Button
              type="button"
              variant="secondary"
              onclick={() => void loadActivities(false)}>Load more</Button
            >
          {/if}
        </div>
        {#if activities.length === 0}
          <p class="empty-state">No activity yet.</p>
        {:else}
          {@const liveActivity = activities.find(
            (entry) => entry.status === "running" || entry.status === "queued",
          )}
          {#if !selectedActivityId && liveActivity}
            <div class="live-view-row">
              <Button
                type="button"
                variant="secondary"
                onclick={() => void openActivity(liveActivity.id)}
              >
                Running — view live output
              </Button>
            </div>
          {/if}
          <ul class="activity-list">
            {#each activities as activity (activity.id)}
              <li>
                <button
                  type="button"
                  class:selected={selectedActivityId === activity.id}
                  onclick={() => void openActivity(activity.id)}
                >
                  <div class="activity-row">
                    <span class="activity-summary">{activity.summary}</span>
                    <small class="activity-meta">
                      {activity.status} · Trigger: {activity.trigger} · Source: {activity.source}{activity.triggered_by
                        ? ` (${activity.triggered_by})`
                        : ""}
                    </small>
                    <small class="activity-meta">
                      Started: {activity.started_at
                        ? formatDateTime(activity.started_at)
                        : "queued"}
                      · Duration: {formatActivityDuration(activity)}
                    </small>
                    <small class="activity-meta">
                      Commits: {activity.commit_before
                        ? activity.commit_before.slice(0, 7)
                        : "—"} → {activity.commit_after
                        ? activity.commit_after.slice(0, 7)
                        : "—"}
                    </small>
                  </div>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="log-panel">
        <div class="panel-header">
          <h2>Output</h2>
          {#if selectedActivityId}
            <div class="inline-actions">
              <Button type="button" variant="secondary" onclick={handleCopyLog}
                >Copy</Button
              >
              <Button
                type="button"
                variant="secondary"
                onclick={handleDownloadLog}>Download</Button
              >
              <Button
                type="button"
                variant="secondary"
                onclick={() => (autoScroll = !autoScroll)}
              >
                {autoScroll ? "Auto-scroll on" : "Auto-scroll off"}
              </Button>
            </div>
          {/if}
        </div>
        <div class="log-shell" bind:this={logViewport}>
          {#if activityLoading}
            <p>Loading output…</p>
          {:else}
            <CodeBlock
              code={activityLog || "No output available."}
              language="text"
              copyable
            />
          {/if}
        </div>
      </div>
    </div>
  {/if}
</section>

<AlertDialog
  bind:open={deleteOpen}
  title="Delete repository"
  description={`Delete ${repository?.name ?? "this repository"}?`}
  size="sm"
  confirmLabel="Delete"
  cancelLabel="Cancel"
  confirmVariant="danger"
  onConfirm={() => void handleDelete()}
>
  <div class="delete-dialog">
    <label>
      <input type="checkbox" bind:checked={deleteFiles} />
      Also delete files on disk ({repository?.path ?? "/repos"})
    </label>
    {#if deleteError}<p class="error">{deleteError}</p>{/if}
  </div>
</AlertDialog>

<style>
  .page-shell {
    width: min(100%, 75rem);
    margin: 0 auto;
    padding: 2rem 0 3rem;
  }

  .repo-header,
  .header-actions,
  .status-row,
  .panel-header,
  .inline-actions,
  .secret-actions-row {
    display: flex;
    align-items: center;
  }

  .repo-header {
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1.5rem;
  }

  .status-row {
    gap: 0.5rem;
    margin-bottom: 0.5rem;
    text-transform: capitalize;
  }

  .header-actions {
    gap: 0.75rem;
    flex-wrap: wrap;
  }

  .meta-grid {
    display: grid;
    grid-template-columns: 1.4fr 1fr;
    gap: 1rem;
    margin-bottom: 1.5rem;
  }

  .info-panel {
    border: 1px solid var(--rw-outline);
    border-radius: 0.8rem;
    background: var(--rw-surface-raised);
    padding: 1rem 1.1rem;
  }

  .info-panel h2,
  .activities-panel h2,
  .log-panel h2 {
    margin: 0 0 1rem;
  }

  dl {
    display: grid;
    gap: 0.7rem;
    margin: 0;
  }

  dl div {
    display: grid;
    grid-template-columns: 8rem minmax(0, 1fr);
    gap: 0.75rem;
  }

  dt {
    color: var(--rw-on-surface-muted);
    font-weight: 600;
  }

  dd {
    margin: 0;
    font-family: var(--rw-font-mono);
    word-break: break-word;
  }

  .mono {
    font-family: var(--rw-font-mono);
  }

  .muted {
    color: var(--rw-on-surface-muted);
  }

  .activity-layout {
    display: grid;
    grid-template-columns: minmax(0, 26rem) minmax(0, 1fr);
    gap: 1rem;
    margin-top: 1.25rem;
  }

  .activities-panel,
  .log-panel {
    border: 1px solid var(--rw-outline);
    border-radius: 0.8rem;
    background: var(--rw-surface-raised);
    padding: 1rem 1.1rem;
  }

  .panel-header {
    justify-content: space-between;
    gap: 0.75rem;
    margin-bottom: 1rem;
  }

  .inline-actions {
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .secret-actions-row {
    margin-top: 0.8rem;
  }

  .activity-list {
    display: grid;
    gap: 0.75rem;
    list-style: none;
    padding: 0;
    margin: 0;
  }

  .activity-list button {
    width: 100%;
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    border: 1px solid var(--rw-outline);
    border-radius: 0.55rem;
    background: transparent;
    padding: 0.7rem 0.75rem;
    text-align: left;
    color: var(--rw-on-surface);
    cursor: pointer;
  }

  .activity-list button.selected {
    border-color: var(--rw-primary);
    background: var(--rw-primary-soft);
  }

  .activity-list small {
    color: var(--rw-on-surface-muted);
  }

  .activity-row {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    width: 100%;
    gap: 0.2rem;
    text-align: left;
  }

  .activity-summary {
    font-weight: 600;
  }

  .activity-meta {
    opacity: 0.8;
  }

  .live-view-row {
    margin-bottom: 0.75rem;
  }

  .log-shell {
    min-height: 18rem;
    max-height: 36rem;
    overflow-y: auto;
  }

  .delete-dialog {
    display: grid;
    gap: 0.7rem;
  }

  .delete-dialog label {
    display: flex;
    align-items: center;
    gap: 0.65rem;
  }

  .error {
    color: var(--rw-danger-text);
  }

  .empty-state {
    color: var(--rw-on-surface-muted);
    padding: 1rem 0;
  }

  .empty-state.error {
    color: var(--rw-danger-text);
  }

  @media (max-width: 767px) {
    .meta-grid,
    .activity-layout {
      grid-template-columns: 1fr;
    }

    .repo-header {
      flex-direction: column;
      align-items: flex-start;
    }
  }
</style>
