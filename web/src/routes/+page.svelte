<script lang="ts">
  import { Button, PageHeader, Separator, StatusDot } from "@runwisp/ui";
  import type { PageProps } from "./$types";

  let { data }: PageProps = $props();
</script>

<svelte:head>
  <title>Workspace · GitSync</title>
</svelte:head>

<section class="overview">
  <PageHeader
    title="Your repositories, in sync."
    subtitle="A calm command surface for the code you keep close."
    class="page-header"
  >
    {#snippet actions()}
      <Button
        variant="secondary"
        disabled
        aria-label="Add repository coming in the next phase"
      >
        Add repository
      </Button>
    {/snippet}
  </PageHeader>

  <div class="welcome">
    <div class="welcome-rail" aria-hidden="true">
      <span class="rail-node active"><StatusDot tone="success" /></span>
      <span class="rail-line"></span>
      <span class="rail-node"></span>
    </div>
    <div class="welcome-copy">
      <p class="welcome-label">Workspace ready</p>
      <h2>Nothing is running yet.</h2>
      <p>
        Once repositories are connected, this space will keep their latest
        activity and sync health in view.
      </p>
      <Separator />
      <p class="welcome-footnote">
        Server <code>{data.meta?.version ?? "unknown"}</code>
      </p>
    </div>
  </div>
</section>

<style>
  .overview {
    width: min(100%, 70rem);
    margin: 0 auto;
  }

  .welcome {
    display: grid;
    grid-template-columns: 1.25rem minmax(0, 36rem);
    gap: 1rem;
    margin-top: clamp(3rem, 8vh, 5.5rem);
  }

  .welcome-rail {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding-top: 0.3rem;
  }

  .rail-node {
    display: grid;
    width: 1.15rem;
    height: 1.15rem;
    flex: 0 0 auto;
    place-items: center;
    border: 1px solid var(--rw-outline-hover);
    border-radius: 50%;
    background: var(--rw-surface);
  }

  .rail-node.active {
    border-color: var(--rw-success-soft-border);
    background: var(--rw-success-soft);
  }

  .rail-line {
    width: 1px;
    min-height: 5rem;
    flex: 1;
    background: var(--rw-outline);
  }

  .welcome-copy {
    max-width: 34rem;
    padding: 0.1rem 0 0.5rem;
  }

  .welcome-label {
    margin: 0 0 0.6rem;
    color: var(--rw-success-soft-text);
    font-family: var(--rw-font-mono);
    font-size: 0.72rem;
  }

  .welcome-copy h2 {
    margin: 0;
    color: var(--rw-on-surface);
    font-family: var(--rw-font-mono);
    font-size: clamp(1.25rem, 2vw, 1.65rem);
    font-weight: 600;
    letter-spacing: -0.04em;
    line-height: 1.25;
  }

  .welcome-copy > p:not(.welcome-label, .welcome-footnote) {
    max-width: 31rem;
    margin: 0.8rem 0 1.4rem;
    color: var(--rw-on-surface-muted);
    font-size: 0.95rem;
    line-height: 1.7;
  }

  .welcome-footnote {
    margin: 0.9rem 0 0;
    color: var(--rw-on-surface-faint);
    font-size: 0.75rem;
  }

  .welcome-footnote code {
    color: var(--rw-on-surface-muted);
    font-family: var(--rw-font-mono);
  }

  @media (max-width: 767px) {
    .overview :global(.page-header) {
      align-items: flex-start;
    }

    .welcome {
      margin-top: 3rem;
    }
  }
</style>
