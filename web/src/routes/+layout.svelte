<script lang="ts">
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
          <span class="brand-mark" aria-hidden="true"><span></span></span>
          <span>GitSync</span>
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
          <span>Git sync status</span>
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

<style>
  .app-frame {
    min-height: 100dvh;
    background: var(--rw-surface);
  }

  .topbar {
    position: relative;
    z-index: 5;
    display: flex;
    height: 4.25rem;
    align-items: center;
    justify-content: space-between;
    border-bottom: 1px solid var(--rw-outline);
    background: var(--rw-surface-raised);
    padding: 0 1.5rem;
  }

  .topbar-brand,
  .topbar-controls,
  .brand-link {
    display: flex;
    align-items: center;
  }

  .topbar-brand {
    min-width: 0;
    gap: 1rem;
  }

  .brand-link {
    gap: 0.65rem;
    color: var(--rw-on-surface);
    font-family: var(--rw-font-mono);
    font-size: 0.95rem;
    font-weight: 700;
    letter-spacing: -0.04em;
    text-decoration: none;
  }

  .brand-mark {
    display: grid;
    width: 1.8rem;
    height: 1.8rem;
    place-items: center;
    border: 1px solid var(--rw-primary);
    border-radius: 0.35rem;
    background: var(--rw-primary-soft);
    transform: rotate(-8deg);
  }

  .brand-mark span {
    width: 0.65rem;
    height: 0.65rem;
    border-radius: 50%;
    background: var(--rw-primary);
    box-shadow: 0.35rem 0.18rem 0 -0.06rem var(--rw-primary);
  }

  .brand-divider {
    width: 1px;
    height: 1.25rem;
    background: var(--rw-outline);
  }

  .brand-section,
  .user-email {
    color: var(--rw-on-surface-muted);
    font-size: 0.8rem;
  }

  .topbar-controls {
    gap: 0.9rem;
  }

  .workspace {
    display: grid;
    min-height: calc(100dvh - 4.25rem);
    grid-template-columns: 17rem minmax(0, 1fr);
  }

  .sidebar {
    display: flex;
    min-width: 0;
    flex-direction: column;
    border-right: 1px solid var(--rw-outline);
    background: var(--rw-surface-raised);
    padding: 1rem 0.85rem 0.8rem;
  }

  .sidebar-top {
    padding: 0.25rem 0.5rem 0.75rem;
  }

  .dashboard-link {
    display: inline-flex;
    width: fit-content;
    color: var(--rw-primary);
    font-weight: 600;
    text-decoration: none;
  }

  .sidebar-actions {
    display: grid;
    gap: 0.75rem;
    padding: 0 0.25rem 0.9rem;
  }

  .sidebar-buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .repository-list-slot {
    display: flex;
    min-height: 12rem;
    flex: 1 1 auto;
    flex-direction: column;
    gap: 0.75rem;
    padding: 0 0.1rem;
  }

  .slot-empty,
  .slot-copy {
    color: var(--rw-on-surface-muted);
  }

  .slot-copy {
    display: grid;
    gap: 0.55rem;
    padding: 0.75rem 0.5rem;
    border: 1px dashed var(--rw-outline);
    border-radius: 0.75rem;
    background: var(--rw-surface);
  }

  .slot-copy p {
    margin: 0;
  }

  .slot-copy a {
    color: var(--rw-primary);
    font-weight: 600;
    text-decoration: none;
  }

  .repo-nav {
    display: grid;
    gap: 0.35rem;
  }

  .repo-link {
    display: flex;
    align-items: center;
    gap: 0.7rem;
    border: 1px solid transparent;
    border-radius: 0.65rem;
    padding: 0.7rem 0.65rem;
    color: var(--rw-on-surface);
    text-decoration: none;
    transition:
      border-color 0.18s ease,
      background 0.18s ease;
  }

  .repo-link:hover {
    border-color: var(--rw-outline);
    background: var(--rw-surface);
  }

  .repo-link.active {
    border-color: var(--rw-primary);
    background: var(--rw-primary-soft);
  }

  .repo-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .sidebar-footer {
    display: grid;
    gap: 0.4rem;
    margin-top: 1rem;
    color: var(--rw-on-surface-muted);
    font-size: 0.8rem;
  }

  .sidebar-status {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    color: var(--rw-on-surface);
  }

  .mobile-backdrop {
    position: fixed;
    inset: 0;
    border: 0;
    background: rgba(17, 24, 39, 0.28);
  }

  .mobile-menu-button {
    display: none;
    border: 1px solid var(--rw-outline);
    border-radius: 0.5rem;
    background: transparent;
    color: var(--rw-on-surface);
    font-size: 1.25rem;
  }

  .error {
    color: var(--rw-danger-text);
  }

  @media (max-width: 767px) {
    .workspace {
      grid-template-columns: 1fr;
    }

    .mobile-menu-button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      width: 2.25rem;
      height: 2.25rem;
    }

    .sidebar {
      position: fixed;
      inset: 4.25rem auto 0 0;
      width: min(18rem, 85vw);
      transform: translateX(-120%);
      transition: transform 0.2s ease;
      z-index: 10;
    }

    .sidebar.drawer-open {
      transform: translateX(0);
    }
  }
</style>
