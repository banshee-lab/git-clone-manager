<script lang="ts">
  import "./layout.css";
  import { onMount } from "svelte";
  import { invalidateAll } from "$app/navigation";
  import { page } from "$app/state";
  import { resolve } from "$app/paths";
  import { repositories, repositoryStatusTone } from "#lib/repositories.svelte";
  import {
    Button,
    Dropdown,
    Heading,
    Input,
    Logo,
    Separator,
    StatusDot,
    ThemeToggle,
    ToastContainer,
    toast,
  } from "@runwisp/ui";
  import { ApiError, logout, syncAll } from "#lib/api/client";
  import { events } from "#lib/events.svelte";
  import type { LayoutProps } from "./$types";
  import "../app.css";

  let { data, children }: LayoutProps = $props();
  let sidebarOpen = $state(false);
  let sidebarFilter = $state("");
  let connectionState = $derived(events.connectionState);

  let filteredRepositories = $derived(
    repositories.repositories.filter((repo) => {
      const query = sidebarFilter.trim().toLowerCase();
      if (!query) return true;
      return `${repo.name} ${repo.url}`.toLowerCase().includes(query);
    }),
  );

  $effect(() => {
    try {
      const pathname =
        page?.url?.pathname ??
        (typeof window !== "undefined" ? window.location.pathname : "/");
      const match = /^\/repositories\/([^/]+)(?:\/edit)?$/.exec(pathname);
      repositories.select(match ? match[1] : null);
    } catch {
      const pathname =
        typeof window !== "undefined" ? window.location.pathname : "/";
      const match = /^\/repositories\/([^/]+)(?:\/edit)?$/.exec(pathname);
      repositories.select(match ? match[1] : null);
    }
  });

  onMount(() => {
    void repositories.refresh();
    if (import.meta.env.VITE_MOCK_API !== "true") events.connect();
    return () => events.close();
  });

  async function handleLogout(): Promise<void> {
    try {
      await logout();
      window.location.assign("/auth/login");
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : "Unable to log out.",
      );
    }
  }

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
      await repositories.refresh();
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Unable to run a sync.",
      );
    }
  }
</script>

<svelte:head>
  <title>GitSync</title>
  <meta
    name="description"
    content="A clear view of your Git repositories and their latest syncs."
  />
</svelte:head>

{#if data.bootstrapError}
  <main class="bootstrap-error">
    <div class="error-mark" aria-hidden="true">!</div>
    <Heading level={1} size="2xl">GitSync is unavailable</Heading>
    <p>{data.bootstrapError}</p>
    <Button onclick={() => void invalidateAll()}>Try again</Button>
  </main>
{:else}
  <div class="app-frame">
    <header class="topbar">
      <div class="topbar-brand">
        <button
          class="mobile-menu-button"
          type="button"
          aria-label="Open repository navigation"
          aria-expanded={sidebarOpen}
          onclick={() => (sidebarOpen = !sidebarOpen)}
        >
          <span aria-hidden="true">☰</span>
        </button>
        <a class="brand-link" href={resolve("/")} aria-label="GitSync home">
          <Logo size="sm" />
          <span>GitSync <strong>Manager</strong></span>
        </a>
        <span class="brand-divider" aria-hidden="true"></span>
        <span class="brand-section">Repository operations</span>
      </div>
      <div class="topbar-controls">
        {#if data.user}
          <span class="user-email">{data.user.email}</span>
          <Dropdown
            triggerLabel={data.user.name || data.user.email}
            items={[{ label: "Log out", onClick: handleLogout, danger: true }]}
          />
        {/if}
        <ThemeToggle />
      </div>
    </header>

    {#if connectionState === "reconnecting"}
      <div class="connection-banner" role="status">
        <StatusDot tone="warning" pulse />
        <span
          >Live updates are reconnecting. The page will refresh when the
          connection returns.</span
        >
      </div>
    {/if}

    <div class="workspace">
      {#if sidebarOpen}
        <button
          class="mobile-backdrop"
          type="button"
          aria-label="Close repository navigation"
          onclick={() => (sidebarOpen = false)}
        ></button>
      {/if}
      <aside
        class:drawer-open={sidebarOpen}
        class="sidebar"
        aria-label="Repository navigation"
      >
        <div class="sidebar-top">
          <a href={resolve("/")} class="dashboard-link">Dashboard</a>
        </div>
        <div class="sidebar-actions">
          <Input
            bind:value={sidebarFilter}
            type="search"
            placeholder="Filter repositories"
            aria-label="Filter repositories in the sidebar"
          />
          <div class="sidebar-buttons">
            <Button variant="secondary" onclick={() => void handleSyncAll()}
              >Sync all</Button
            >
            <Button
              onclick={() =>
                (window.location.href = resolve("/repositories/new"))}
              >Add repository</Button
            >
          </div>
        </div>
        <div
          class="repository-list-slot"
          data-testid="repository-list-slot"
          aria-label="Repository list slot"
        >
          {#if repositories.loading}
            <p class="slot-empty">Loading repositories…</p>
          {:else if repositories.error}
            <p class="slot-empty error">{repositories.error}</p>
          {:else if filteredRepositories.length === 0}
            <span class="slot-rail" aria-hidden="true"></span>
            <div class="slot-copy">
              <p>No repositories match.</p>
              <a href={resolve("/repositories/new")}>Add repository</a>
            </div>
          {:else}
            <nav class="repo-nav" aria-label="Repository list">
              {#each filteredRepositories as repo (repo.id)}
                <a
                  href={resolve("/repositories/[id]", { id: repo.id })}
                  class:active={repositories.selectedId === repo.id}
                  class="repo-link"
                >
                  <StatusDot tone={repositoryStatusTone(repo)} />
                  <span class="repo-name">{repo.name}</span>
                </a>
              {/each}
            </nav>
          {/if}
        </div>
        <div class="sidebar-footer">
          <Separator />
          <span class="sidebar-status"
            ><StatusDot tone="success" /> Connected</span
          >
        </div>
      </aside>

      <main class="main-content">
        {@render children()}
      </main>
    </div>
  </div>
{/if}

<ToastContainer />
