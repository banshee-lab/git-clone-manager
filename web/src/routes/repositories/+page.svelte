<script lang="ts">
  import { Button, Heading, Input, StatusDot } from "@runwisp/ui";
  import { onMount } from "svelte";
  import { resolve } from "$app/paths";
  import { syncAll } from "#lib/api/client";
  import { repositories, repositoryStatusTone } from "#lib/repositories.svelte";
  import { toast } from "@runwisp/ui";

  let filter = $state("");

  onMount(() => {
    void repositories.refresh();
  });

  let filteredRepositories = $derived(
    repositories.repositories.filter((repo) => {
      const query = filter.trim().toLowerCase();
      if (!query) return true;
      return `${repo.name} ${repo.url}`.toLowerCase().includes(query);
    }),
  );

  async function handleSyncAll(): Promise<void> {
    try {
      const result = await syncAll();
      const started = result.started.length;
      const skipped = result.skipped.length;
      toast.success(
        started > 0 || skipped > 0
          ? `Sync started for ${started} repository${started === 1 ? "" : "s"}${skipped > 0 ? `; ${skipped} skipped` : ""}.`
          : "No repositories were eligible to sync.",
      );
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Unable to run a sync.",
      );
    }
  }
</script>

<svelte:head>
  <title>Repositories · GitSync</title>
</svelte:head>

<section class="page-shell">
  <header class="toolbar">
    <Heading level={1} size="lg">Repositories</Heading>
    <div class="toolbar-actions">
      <Button variant="secondary" onclick={() => void handleSyncAll()}
        >Sync all</Button
      >
      <Button onclick={() => (window.location.href = "/repositories/new")}
        >Add repository</Button
      >
    </div>
  </header>

  <div class="search-row">
    <Input
      bind:value={filter}
      type="search"
      placeholder="Filter repositories"
      aria-label="Filter repositories"
    />
  </div>

  {#if repositories.loading}
    <p class="empty-state">Loading repositories…</p>
  {:else if repositories.error}
    <p class="empty-state error">{repositories.error}</p>
  {:else if filteredRepositories.length === 0}
    <div class="empty-state card">
      <h2>No repositories match</h2>
      <p>Try a different filter or create a repository.</p>
      <Button onclick={() => (window.location.href = "/repositories/new")}
        >Add repository</Button
      >
    </div>
  {:else}
    <ul class="repo-list" aria-label="Repository list">
      {#each filteredRepositories as repo (repo.id)}
        <li>
          <a
            href={resolve("/repositories/[id]", { id: repo.id })}
            class="repo-card"
            class:active={repositories.selectedId === repo.id}
          >
            <span class="status"
              ><StatusDot tone={repositoryStatusTone(repo)} /></span
            >
            <span class="meta">
              <strong>{repo.name}</strong>
              <small>{repo.branch}</small>
            </span>
            <span class="path">{repo.path}</span>
          </a>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .page-shell {
    width: min(100%, 68rem);
    margin: 0 auto;
    padding: 2rem 0 3rem;
  }
  .toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    margin-bottom: 1rem;
  }
  .toolbar-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.75rem;
  }
  .search-row {
    margin-bottom: 1.25rem;
  }
  .repo-list {
    display: grid;
    gap: 0.85rem;
    list-style: none;
    padding: 0;
    margin: 0;
  }
  .repo-card {
    display: grid;
    grid-template-columns: auto minmax(0, 1.2fr) minmax(0, 1.6fr);
    align-items: center;
    gap: 1rem;
    border: 1px solid var(--rw-outline);
    border-radius: 0.75rem;
    background: var(--rw-surface-raised);
    padding: 1rem 1.1rem;
    text-decoration: none;
    color: var(--rw-on-surface);
  }
  .repo-card.active {
    border-color: var(--rw-primary);
    background: var(--rw-primary-soft);
  }
  .meta {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .meta strong {
    font-weight: 600;
  }
  .meta small,
  .path {
    color: var(--rw-on-surface-muted);
    font-family: var(--rw-font-mono);
    font-size: 0.72rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty-state {
    display: grid;
    place-items: center;
    min-height: 12rem;
    padding: 2rem;
    border: 1px dashed var(--rw-outline-hover);
    border-radius: 0.9rem;
    color: var(--rw-on-surface-muted);
    text-align: center;
  }
  .empty-state.card {
    background: var(--rw-surface-raised);
  }
  .empty-state h2 {
    margin: 0 0 0.35rem;
    color: var(--rw-on-surface);
  }
  .empty-state.error {
    color: var(--rw-danger-text);
  }
  @media (max-width: 767px) {
    .toolbar {
      flex-direction: column;
      align-items: flex-start;
    }
    .repo-card {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .path {
      grid-column: 2;
    }
  }
</style>
