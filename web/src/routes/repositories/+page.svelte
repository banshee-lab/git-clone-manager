<script lang="ts">
  import "./repositories.css";
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
  <title>Repositories · Git Clone Manager</title>
</svelte:head>

<section class="repositories-page page-shell">
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
