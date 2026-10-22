<script lang="ts">
  import "./dashboard.css";
  import { onDestroy, onMount } from "svelte";
  import { resolve } from "$app/paths";
  import {
    Button,
    CodeBlock,
    PageHeader,
    Separator,
    StatusDot,
    toast,
  } from "@runwisp/ui";
  import { dashboard } from "#lib/dashboard.svelte";
  import { createLogStream } from "#lib/log-stream.svelte";
  import { statusTone } from "#lib/utils";
  import { formatDuration, relativeTime } from "#lib/utils";
  import type { ActivitySummary } from "#lib/api/types";
  import { ApiError } from "#lib/api/client";

  // ---------- elapsed timer ----------
  let now = $state(Date.now());

  onMount(() => {
    // Initial load
    void dashboard.refresh();

    // Tick every second for elapsed timers
    const timer = setInterval(() => {
      now = Date.now();
    }, 1000);
    return () => clearInterval(timer);
  });

  function elapsedMs(act: ActivitySummary): number {
    const started = act.started_at
      ? new Date(act.started_at).getTime()
      : new Date(act.created_at).getTime();
    const finished = act.finished_at
      ? new Date(act.finished_at).getTime()
      : now;
    return Math.max(0, finished - started);
  }

  // ---------- live console ----------
  let selectedActivityId = $state<string | null>(null);
  let selectedActivitySnapshot = $state<ActivitySummary | null>(null);
  let liveStream = $state<ReturnType<typeof createLogStream> | null>(null);
  let logViewport = $state<HTMLDivElement | null>(null);
  let consoleOpener = $state<HTMLElement | null>(null);
  let dashboardShell = $state<HTMLElement | null>(null);

  onDestroy(() => liveStream?.close());
  let autoScroll = $state(true);
  let consoleLog = $state("");

  /** Open the live console for a running/queued activity. */
  function openConsole(activity: ActivitySummary, opener: HTMLElement): void {
    const activityId = activity.id;
    if (selectedActivityId === activityId) return;
    closeConsole(false);
    consoleOpener = opener;
    selectedActivitySnapshot = activity;
    selectedActivityId = activityId;
    const stream = createLogStream(activityId);
    liveStream = stream;
    stream.subscribe(() => {
      consoleLog = stream.log;
    });
    consoleLog = stream.log;
    queueMicrotask(() =>
      document
        .querySelector<HTMLButtonElement>(
          '.console-sheet button[aria-label="Close console"]',
        )
        ?.focus(),
    );
  }

  function closeConsole(restoreFocus = true): void {
    if (liveStream) {
      liveStream.close();
      liveStream = null;
    }
    selectedActivityId = null;
    selectedActivitySnapshot = null;
    consoleLog = "";
    autoScroll = true;
    if (restoreFocus) {
      queueMicrotask(() => {
        if (consoleOpener?.isConnected) consoleOpener.focus();
        else dashboardShell?.focus();
      });
    }
  }

  // Auto-scroll when log updates
  $effect(() => {
    if (!autoScroll || !logViewport) return;
    void consoleLog;
    queueMicrotask(() => {
      if (logViewport) logViewport.scrollTop = logViewport.scrollHeight;
    });
  });

  /** The running/queued activity being viewed in the console, if any. */
  let selectedActivity = $derived(
    dashboard.running.find((a) => a.id === selectedActivityId) ??
      selectedActivitySnapshot,
  );

  // ---------- retry ----------
  let retryingIds = $state<Set<string>>(new Set());

  async function handleRetry(
    repositoryId: string,
    repositoryName: string,
  ): Promise<void> {
    retryingIds = new Set([...retryingIds, repositoryId]);
    try {
      await dashboard.retrySync(repositoryId);
      toast.success(`Sync queued for ${repositoryName}.`);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast.error(
          err.message || `${repositoryName} is already running or queued.`,
        );
      } else {
        toast.error(
          err instanceof Error
            ? err.message
            : `Unable to retry ${repositoryName}.`,
        );
      }
    } finally {
      retryingIds = new Set(
        [...retryingIds].filter((id) => id !== repositoryId),
      );
    }
  }

  // ---------- helpers ----------
  const STAGE_LABELS: Record<string, string> = {
    clone: "Clone",
    fetch: "Fetch",
    checkout: "Checkout",
    merge: "Merge",
    reset: "Reset",
    post_command: "Post command",
    internal: "Internal",
    interrupted: "Interrupted",
  };

  function stageLabel(stage: string): string {
    return STAGE_LABELS[stage] ?? stage;
  }

  function sourceLabel(trigger: string, source: string): string {
    if (trigger === "webhook") {
      if (source === "github") return "GitHub webhook";
      if (source === "gitlab") return "GitLab webhook";
      return "Webhook";
    }
    return "Manual";
  }

  function handleConsoleKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      closeConsole();
      return;
    }
    if (event.key !== "Tab") return;
    const controls = Array.from(
      document.querySelectorAll<HTMLElement>(
        ".console-sheet button:not(:disabled), .console-sheet a[href], .console-sheet [tabindex='0']",
      ),
    );
    if (controls.length === 0) return;
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }
</script>

<svelte:head>
  <title>Dashboard · Git Clone Manager</title>
</svelte:head>

<section class="dashboard-shell" bind:this={dashboardShell} tabindex="-1">
  <PageHeader
    title="Dashboard"
    subtitle="Live overview of your repository operations."
    class="page-header"
  >
    {#snippet actions()}
      <Button variant="secondary" onclick={() => void dashboard.refresh()}>
        Refresh
      </Button>
    {/snippet}
  </PageHeader>

  {#if dashboard.loading && dashboard.repositoryCount === 0}
    <p class="empty-note" role="status">Loading dashboard…</p>
  {:else if dashboard.error && dashboard.repositoryCount === 0}
    <p class="empty-note error" role="alert">{dashboard.error}</p>
  {:else}
    <!-- ═══ Stat cards ═══ -->
    <div class="stat-row" aria-label="Dashboard statistics">
      <div class="stat-card" data-testid="stat-repositories">
        <span class="stat-value">{dashboard.repositoryCount}</span>
        <span class="stat-label">
          <a href={resolve("/repositories")} class="stat-link">Repositories</a>
        </span>
      </div>
      <div class="stat-card" data-testid="stat-running">
        <span class="stat-value">{dashboard.running.length}</span>
        <span class="stat-label">Running now</span>
      </div>
      <div
        class="stat-card"
        class:stat-card--alert={dashboard.attention.length > 0}
        data-testid="stat-attention"
      >
        <span class="stat-value">{dashboard.attention.length}</span>
        <span class="stat-label">Needs attention</span>
      </div>
      <div class="stat-card" data-testid="stat-last-sync">
        <span class="stat-value stat-value--sm">
          {dashboard.lastSyncTime
            ? relativeTime(dashboard.lastSyncTime, now)
            : "—"}
        </span>
        <span class="stat-label">Last sync</span>
      </div>
    </div>

    <!-- ═══ First-run CTA ═══ -->
    {#if dashboard.repositoryCount === 0}
      <div class="first-run" data-testid="first-run-cta">
        <p class="first-run-lead">No repositories yet.</p>
        <p class="first-run-body">
          Add your first repository to start tracking syncs.
        </p>
        <Button
          onclick={() => (window.location.href = resolve("/repositories/new"))}
        >
          Add repository
        </Button>
      </div>
    {:else}
      <div class="panels-grid">
        <!-- ═══ Running now ═══ -->
        <section aria-labelledby="panel-running-title" class="panel">
          <h2 id="panel-running-title" class="panel-title">Running now</h2>
          {#if dashboard.running.length === 0}
            <p class="empty-note" data-testid="running-empty">
              Nothing running.
            </p>
          {:else}
            <ul
              class="item-list"
              aria-live="polite"
              aria-label="Running activities"
            >
              {#each dashboard.running as act (act.id)}
                <li>
                  <button
                    type="button"
                    class="item-row"
                    class:item-row--selected={selectedActivityId === act.id}
                    onclick={(event) => openConsole(act, event.currentTarget)}
                    aria-pressed={selectedActivityId === act.id}
                  >
                    <span class="item-dot">
                      <StatusDot
                        tone={statusTone(act.status)}
                        pulse={act.status === "running"}
                      />
                    </span>
                    <span class="item-body">
                      <span class="item-name">{act.repository_name}</span>
                      <span class="item-meta">
                        {sourceLabel(act.trigger, act.source)} · {act.status}
                        · {formatDuration(elapsedMs(act))}
                      </span>
                    </span>
                    <span class="item-chevron" aria-hidden="true">›</span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <!-- ═══ Needs attention ═══ -->
        <section aria-labelledby="panel-attention-title" class="panel">
          <h2 id="panel-attention-title" class="panel-title">
            Needs attention
          </h2>
          {#if dashboard.attention.length === 0}
            <p class="empty-note" data-testid="attention-empty">
              All good — nothing needs attention.
            </p>
          {:else}
            <ul
              class="item-list"
              aria-live="polite"
              aria-label="Repositories needing attention"
            >
              {#each dashboard.attention as item (item.repository_id)}
                <li class="attention-item">
                  <!-- eslint-disable svelte/no-navigation-without-resolve -->
                  <a
                    href={resolve("/repositories/[id]", {
                      id: item.repository_id,
                    }) +
                      "?activity=" +
                      encodeURIComponent(item.activity_id)}
                    class="attention-link"
                  >
                    <span class="item-dot">
                      <StatusDot tone="danger" />
                    </span>
                    <span class="item-body">
                      <span class="item-name">{item.repository_name}</span>
                      <span class="item-meta">
                        <span class="stage-badge"
                          >{stageLabel(item.failed_stage || item.status)}</span
                        >
                        {item.summary}
                      </span>
                      <span class="item-meta item-meta--faint">
                        {item.finished_at
                          ? relativeTime(item.finished_at, now)
                          : "—"}
                      </span>
                    </span>
                  </a>
                  <!-- eslint-enable svelte/no-navigation-without-resolve -->
                  <div class="attention-retry">
                    <Button
                      variant="secondary"
                      size="xs"
                      loading={retryingIds.has(item.repository_id)}
                      disabled={retryingIds.has(item.repository_id)}
                      onclick={() =>
                        void handleRetry(
                          item.repository_id,
                          item.repository_name,
                        )}
                    >
                      Retry
                    </Button>
                  </div>
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <!-- ═══ Recent activity ═══ -->
        <section aria-labelledby="panel-recent-title" class="panel panel--wide">
          <h2 id="panel-recent-title" class="panel-title">Recent activity</h2>
          {#if dashboard.recent.length === 0}
            <p class="empty-note">No recent activity.</p>
          {:else}
            <ul
              class="item-list recent-list"
              aria-live="polite"
              aria-label="Recent activities"
            >
              {#each dashboard.recent as act (act.id)}
                <li>
                  <!-- eslint-disable svelte/no-navigation-without-resolve -->
                  <a
                    href={resolve("/repositories/[id]", {
                      id: act.repository_id,
                    }) +
                      "?activity=" +
                      encodeURIComponent(act.id)}
                    class="recent-row"
                  >
                    <span class="item-dot">
                      <StatusDot tone={statusTone(act.status)} />
                    </span>
                    <span class="recent-repo">{act.repository_name}</span>
                    <span class="recent-meta">{act.status}</span>
                    <span class="recent-meta"
                      >{sourceLabel(act.trigger, act.source)}</span
                    >
                    <span class="recent-meta">
                      {act.finished_at
                        ? relativeTime(act.finished_at, now)
                        : act.started_at
                          ? relativeTime(act.started_at, now)
                          : relativeTime(act.created_at, now)}
                    </span>
                    <span class="recent-meta">
                      {act.finished_at && act.started_at
                        ? formatDuration(
                            new Date(act.finished_at).getTime() -
                              new Date(act.started_at).getTime(),
                          )
                        : act.status === "running" || act.status === "queued"
                          ? formatDuration(elapsedMs(act))
                          : "—"}
                    </span>
                  </a>
                  <!-- eslint-enable svelte/no-navigation-without-resolve -->
                </li>
              {/each}
            </ul>
          {/if}
        </section>
      </div>
    {/if}
  {/if}
</section>

<!-- ═══ Live console side sheet ═══ -->
{#if selectedActivityId}
  {@const connectionState = liveStream?.connectionState ?? "closed"}
  {@const finalStatus = liveStream?.activityStatus}
  {@const isDone =
    finalStatus !== undefined &&
    finalStatus !== "queued" &&
    finalStatus !== "running"}
  <button
    type="button"
    class="console-backdrop"
    aria-label="Close live console"
    onclick={() => closeConsole()}
  ></button>
  <dialog
    open
    class="console-sheet"
    aria-modal="true"
    aria-label="Live console"
    tabindex="-1"
    onkeydown={handleConsoleKeydown}
  >
    <div class="console-header">
      <div class="console-meta">
        <span class="console-title">
          {selectedActivity?.repository_name ?? "Live output"}
        </span>
        {#if isDone}
          <span class="console-state done">
            <StatusDot
              tone={finalStatus === "success" ? "success" : "danger"}
            />
            <span>{finalStatus}</span>
          </span>
        {:else if connectionState === "open"}
          <span class="console-state live">
            <StatusDot tone="primary" pulse />
            <span>Live</span>
          </span>
        {:else if connectionState === "reconnecting"}
          <span class="console-state reconnecting">
            <StatusDot tone="warning" />
            <span>Reconnecting…</span>
          </span>
        {:else if connectionState === "closed"}
          <span class="console-state disconnected">Disconnected</span>
        {:else}
          <span class="console-state connecting">Connecting…</span>
        {/if}
      </div>
      <div class="console-actions">
        {#if isDone && selectedActivity}
          <Button
            variant="secondary"
            size="xs"
            onclick={() => {
              if (selectedActivity) {
                window.location.href =
                  resolve("/repositories/[id]", {
                    id: selectedActivity.repository_id,
                  }) +
                  "?activity=" +
                  encodeURIComponent(selectedActivity.id);
              }
            }}
          >
            Open repository
          </Button>
        {/if}
        <Button
          variant="secondary"
          size="xs"
          onclick={() => (autoScroll = !autoScroll)}
        >
          {autoScroll ? "Auto-scroll on" : "Auto-scroll off"}
        </Button>
        <Button
          variant="ghost"
          size="xs"
          onclick={() => closeConsole()}
          aria-label="Close console"
        >
          ✕
        </Button>
      </div>
    </div>
    <Separator />
    <!-- sr-only live region for accessibility -->
    <div role="status" aria-live="polite" aria-atomic="false" class="sr-only">
      {#if isDone}
        Activity {finalStatus}.
      {:else if connectionState === "open"}
        Console connected. Streaming activity.
      {:else if connectionState === "reconnecting"}
        Console connection lost. Reconnecting.
      {:else if connectionState === "connecting"}
        Connecting to activity console.
      {/if}
    </div>
    <div
      class="console-log-shell"
      bind:this={logViewport}
      role="log"
      aria-label="Activity output"
      aria-live="off"
    >
      <CodeBlock
        code={consoleLog ||
          (connectionState === "open" || connectionState === "connecting"
            ? "Waiting for output…"
            : "No output.")}
        language="text"
        copyable
      />
    </div>
  </dialog>
{/if}
