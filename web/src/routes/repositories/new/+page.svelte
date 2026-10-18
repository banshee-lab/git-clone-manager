<script lang="ts">
  import { onMount } from "svelte";
  import { Button, Heading, Input, Textarea, Toggle } from "@runwisp/ui";
  import { createRepository, getMeta } from "#lib/api/client";
  import { toast } from "@runwisp/ui";
  import {
    buildRepositoryPayload,
    suggestClonePath,
    validateRepositoryForm,
  } from "#lib/repository-form";

  let form = $state({
    name: "",
    url: "",
    branch: "",
    path: "",
    access_username: "git",
    access_token: "",
    post_command: "",
    post_command_timeout_seconds: 0,
    hard_reset: false,
    webhook_secret: "",
  });
  let errors = $state<Record<string, string>>({});
  let reposRoot = $state("/repos");
  let pathTouched = $state(false);
  let saving = $state(false);

  onMount(async () => {
    try {
      const meta = await getMeta();
      reposRoot = meta.repos_root || "/repos";
      if (!pathTouched && form.path === "") {
        form.path = suggestClonePath(form.name, reposRoot);
      }
    } catch {
      reposRoot = "/repos";
    }
  });

  function handleNameChange(value: string): void {
    form.name = value;
    if (!pathTouched && !form.path) {
      form.path = suggestClonePath(value, reposRoot);
    }
  }

  async function submit(): Promise<void> {
    const nextErrors = validateRepositoryForm(form);
    errors = nextErrors;
    if (Object.keys(nextErrors).length > 0) {
      toast.error("Please fix the highlighted repository settings.");
      return;
    }

    saving = true;
    try {
      const payload = buildRepositoryPayload(form, "create");
      const repository = await createRepository(payload);
      window.location.assign(`/repositories/${repository.id}`);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Unable to create repository.";
      toast.error(message);
    } finally {
      saving = false;
    }
  }
</script>

<svelte:head><title>Create repository · GitSync</title></svelte:head>

<section class="page-shell">
  <Heading level={1} size="lg">Add repository</Heading>
  <form
    class="form-grid"
    onsubmit={(event) => {
      event.preventDefault();
      void submit();
    }}
  >
    <div class="field">
      <label for="name">Name</label><Input
        id="name"
        bind:value={form.name}
        oninput={(event) =>
          handleNameChange((event.currentTarget as HTMLInputElement).value)}
        aria-invalid={Boolean(errors.name)}
      />{#if errors.name}<small class="error">{errors.name}</small>{/if}
    </div>
    <div class="field">
      <label for="url">Repository URL</label><Input
        id="url"
        bind:value={form.url}
        aria-invalid={Boolean(errors.url)}
      />{#if errors.url}<small class="error">{errors.url}</small>{/if}
    </div>
    <div class="field">
      <label for="branch">Branch</label><Input
        id="branch"
        bind:value={form.branch}
        placeholder="default branch"
        aria-invalid={Boolean(errors.branch)}
      />{#if errors.branch}<small class="error">{errors.branch}</small>{/if}
    </div>
    <div class="field">
      <label for="path">Clone path</label><Input
        id="path"
        bind:value={form.path}
        aria-invalid={Boolean(errors.path)}
        onfocus={() => (pathTouched = true)}
      />{#if errors.path}<small class="error">{errors.path}</small>{/if}<small
        class="hint"
        >Suggested default: {suggestClonePath(
          form.name || "repository",
          reposRoot,
        )}</small
      >
    </div>
    <div class="field">
      <label for="access_username">Access username</label><Input
        id="access_username"
        bind:value={form.access_username}
        aria-invalid={Boolean(errors.access_username)}
      />{#if errors.access_username}<small class="error"
          >{errors.access_username}</small
        >{/if}
    </div>
    <div class="field">
      <label for="access_token">Access token</label><Input
        id="access_token"
        bind:value={form.access_token}
        type="password"
        placeholder="Optional"
      />
    </div>
    <div class="field field-span-2">
      <label for="post_command">Post command</label><Textarea
        id="post_command"
        bind:value={form.post_command}
        rows={5}
        placeholder="Optional shell command to run after sync"
      />
    </div>
    <div class="field">
      <label for="timeout">Timeout seconds</label><Input
        id="timeout"
        bind:value={form.post_command_timeout_seconds}
        type="number"
        min="0"
        step="1"
      />
    </div>
    <div class="field">
      <label for="hard_reset">Hard reset</label><Toggle
        bind:checked={form.hard_reset}
        label="Discard local changes on sync"
      />
    </div>
    <div class="field field-span-2">
      <label for="webhook_secret">Webhook secret</label><Input
        id="webhook_secret"
        bind:value={form.webhook_secret}
        type="password"
        placeholder="Optional override; leave blank to use the global secret"
      />
    </div>
    <div class="actions field-span-2">
      <Button
        variant="secondary"
        type="button"
        onclick={() => (window.location.href = "/repositories")}>Cancel</Button
      ><Button type="submit" loading={saving}>Create repository</Button>
    </div>
  </form>
</section>

<style>
  .page-shell {
    width: min(100%, 48rem);
    margin: 0 auto;
    padding: 2rem 0 3rem;
  }
  .form-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 1.1rem;
    margin-top: 1.5rem;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.45rem;
  }
  .field-span-2 {
    grid-column: 1 / -1;
  }
  label {
    font-weight: 600;
    color: var(--rw-on-surface);
  }
  .hint,
  .error {
    font-size: 0.75rem;
  }
  .error {
    color: var(--rw-danger-text);
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.75rem;
    margin-top: 0.5rem;
  }
  @media (max-width: 767px) {
    .form-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
