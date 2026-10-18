<script lang="ts">
  import { onMount } from "svelte";
  import { invalidateAll } from "$app/navigation";
  import { resolve } from "$app/paths";
  import {
    Button,
    Dropdown,
    Heading,
    Separator,
    StatusDot,
    ThemeToggle,
    ToastContainer,
    toast,
  } from "@runwisp/ui";
  import { ApiError, logout } from "#lib/api/client";
  import { events } from "#lib/events.svelte";
  import type { LayoutProps } from "./$types";
  import "../app.css";

  let { data, children }: LayoutProps = $props();
  let sidebarOpen = $state(false);
  let connectionState = $derived(events.connectionState);

  onMount(() => {
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
        <div class="sidebar-heading">
          <Heading level={2} size="xs">Repositories</Heading>
        </div>
        <div
          class="repository-list-slot"
          data-testid="repository-list-slot"
          aria-label="Repository list slot"
        >
          <span class="slot-rail" aria-hidden="true"></span>
          <p>Repository list will appear here.</p>
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
    grid-template-columns: 15rem minmax(0, 1fr);
  }

  .sidebar {
    display: flex;
    min-width: 0;
    flex-direction: column;
    border-right: 1px solid var(--rw-outline);
    background: var(--rw-surface-raised);
    padding: 1.5rem 1rem 1rem;
  }

  .sidebar-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 0.5rem 1rem;
  }

  .repository-list-slot {
    position: relative;
    display: flex;
    min-height: 5rem;
    align-items: flex-start;
    gap: 0.75rem;
    border: 1px dashed var(--rw-outline-hover);
    border-radius: 0.25rem;
    padding: 0.85rem 0.75rem;
  }

  .slot-rail {
    width: 0.42rem;
    height: 0.42rem;
    flex: 0 0 auto;
    margin-top: 0.35rem;
    border: 1px solid var(--rw-on-surface-faint);
    border-radius: 50%;
  }

  .repository-list-slot p {
    margin: 0;
    color: var(--rw-on-surface-faint);
    font-size: 0.75rem;
    line-height: 1.5;
  }

  .sidebar-footer {
    display: grid;
    gap: 0.75rem;
    margin-top: auto;
    padding: 1rem 0.5rem 0.25rem;
    color: var(--rw-on-surface-faint);
    font-size: 0.7rem;
  }

  .sidebar-status {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--rw-on-surface-muted);
  }

  .main-content {
    min-width: 0;
    padding: clamp(1.5rem, 4vw, 3.5rem);
  }

  .connection-banner {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 0.65rem;
    border-bottom: 1px solid var(--rw-warning-soft-border);
    background: var(--rw-warning-soft);
    padding: 0.55rem 1rem;
    color: var(--rw-warning-soft-text);
    font-size: 0.8rem;
  }

  .bootstrap-error {
    display: flex;
    min-height: 100dvh;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    gap: 1rem;
    max-width: 35rem;
    margin: 0 auto;
    padding: 2rem;
  }

  .bootstrap-error p {
    margin: 0 0 0.5rem;
    color: var(--rw-on-surface-muted);
  }

  .error-mark {
    display: grid;
    width: 2.3rem;
    height: 2.3rem;
    place-items: center;
    border: 1px solid var(--rw-danger-soft-border);
    border-radius: 50%;
    color: var(--rw-danger-soft-text);
    font-family: var(--rw-font-mono);
  }

  .mobile-menu-button,
  .mobile-backdrop {
    display: none;
  }

  @media (max-width: 767px) {
    .topbar {
      height: 3.75rem;
      padding: 0 0.8rem;
    }

    .topbar-brand {
      gap: 0.65rem;
    }

    .brand-section,
    .brand-divider,
    .user-email {
      display: none;
    }

    .topbar-controls {
      gap: 0.25rem;
    }

    .workspace {
      min-height: calc(100dvh - 3.75rem);
      grid-template-columns: minmax(0, 1fr);
    }

    .mobile-menu-button {
      display: grid;
      width: 2rem;
      height: 2rem;
      place-items: center;
      border: 0;
      border-radius: 0.2rem;
      background: transparent;
      color: var(--rw-on-surface-muted);
      cursor: pointer;
      font-size: 1rem;
    }

    .mobile-menu-button:hover {
      background: var(--rw-surface-sunken);
      color: var(--rw-on-surface);
    }

    .sidebar {
      position: fixed;
      z-index: 12;
      top: 3.75rem;
      bottom: 0;
      left: 0;
      width: min(18rem, calc(100vw - 3rem));
      border-right: 1px solid var(--rw-outline);
      box-shadow: var(--rw-shadow-md);
      transform: translateX(-105%);
      transition: transform 160ms ease-out;
    }

    .sidebar.drawer-open {
      transform: translateX(0);
    }

    .mobile-backdrop {
      position: fixed;
      z-index: 11;
      inset: 3.75rem 0 0;
      display: block;
      border: 0;
      background: var(--rw-backdrop);
      backdrop-filter: blur(2px);
    }

    .main-content {
      padding: 1.5rem 1rem;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .sidebar {
      transition: none;
    }
  }
</style>
