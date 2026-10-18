<script lang="ts">
  import { onMount } from "svelte";
  import {
    AlertDialog,
    Button,
    CodeBlock,
    Heading,
    Separator,
    StatusDot,
  } from "@runwisp/ui";
  import { page } from "$app/state";
  import {
    deleteRepository,
    getActivityLog,
    getRepository,
    listActivities,
    syncRepository,
  } from "#lib/api/client";
  import { repositoryStatusTone } from "#lib/repositories.svelte";
  import { formatDateTime, stripAnsi } from "#lib/utils";
  import type { ActivitySummary, Repository } from "#lib/api/types";

  let repository = $state<Repository | null>(null);
  let activities = $state<ActivitySummary[]>([]);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let deleteOpen = $state(false);
  let deleteFiles = $state(false);
  let deleteError = $state<string | null>(null);
  let selectedActivityId = $state<string | null>(null);
  let activityLog = $state("");
  let activityLoading = $state(false);

  async function loadRepository(): Promise<void> {
    const id = page.params.id;
    if (!id) return;
    loading = true;
    loadError = null;
    try {
      const [repo, list] = await Promise.all([
        getRepository(id),
        listActivities(id),
      ]);
      repository = repo;
      activities = list;
      if (list.length > 0 && !selectedActivityId) {
        selectedActivityId = list[0].id;
      }
    } catch (error) {
      loadError =
        error instanceof Error ? error.message : "Unable to load repository.";
    } finally {
      loading = false;
    }
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

  $effect(() => {
    if (!page.params.id) return;
    void loadRepository();
  });

  $effect(() => {
    if (!selectedActivityId) return;
    void loadActivityLog(selectedActivityId);
  });

  async function handleSyncNow(): Promise<void> {
    if (!repository) return;
    try {
      await syncRepository(repository.id);
      await loadRepository();
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Unable to start sync.";
      if (!message.includes("already")) {
        throw error;
      }
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

  onMount(() => {
    void loadRepository();
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
          <StatusDot tone={repositoryStatusTone(repository)} /><span
            >{repository.last_activity?.status ?? "never"}</span
          >
        </div>
        <Heading level={1} size="lg">{repository.name}</Heading>
      </div>
      <div class="header-actions">
        <Button
          variant="secondary"
          onclick={() => {
            if (repository)
              window.location.href = "/repositories/" + repository.id + "/edit";
          }}>Edit</Button
        >
        <Button variant="danger" onclick={() => (deleteOpen = true)}
          >Delete</Button
        >
        <Button
          onclick={() => void handleSyncNow()}
          disabled={Boolean(repository?.running_activity_id)}>Sync now</Button
        >
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
      </div>
    </div>

    <Separator />

    <div class="activity-layout">
      <div class="activities-panel">
        <h2>Activity</h2>
        {#if activities.length === 0}
          <p class="empty-state">No activity yet.</p>
        {:else}
          <ul class="activity-list">
            {#each activities as activity (activity.id)}
              <li>
                <button
                  type="button"
                  class:selected={selectedActivityId === activity.id}
                  onclick={() => (selectedActivityId = activity.id)}
                >
                  <span>{activity.summary}</span>
                  <small
                    >{activity.status} · {formatDateTime(
                      activity.created_at,
                    )}</small
                  >
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="log-panel">
        <h2>Output</h2>
        <div class="log-shell">
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
    <label
      ><input type="checkbox" bind:checked={deleteFiles} /> Also delete files on
      disk ({repository?.path ?? "/repos"})</label
    >
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
  .status-row {
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
  .log-shell {
    min-height: 18rem;
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
