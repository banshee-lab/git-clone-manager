<script lang="ts">
  import { onMount } from "svelte";
  import { Button, Heading, Input, Textarea, Toggle, toast } from "@runwisp/ui";
  import { page } from "$app/state";
  import { resolve } from "$app/paths";
  import { getRepository, updateRepository } from "#lib/api/client";
  import {
    buildRepositoryPayload,
    generateWebhookSecret,
    suggestClonePath,
    validateRepositoryForm,
  } from "#lib/repository-form";
  import type { SecretMode, TokenMode } from "#lib/repository-form";

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
  let tokenMode = $state<TokenMode>("keep");
  let secretMode = $state<SecretMode>("keep");
  let secretVisible = $state(false);

  onMount(async () => {
    const id = page.params.id;
    if (!id) return;
    try {
      const repository = await getRepository(id);
      form.name = repository.name;
      form.url = repository.url;
      form.branch = repository.branch;
      form.path = repository.path;
      form.access_username = repository.access_username;
      form.post_command = repository.post_command ?? "";
      form.post_command_timeout_seconds =
        repository.post_command_timeout_seconds ?? 0;
      form.hard_reset = repository.hard_reset;
      form.webhook_secret = repository.webhook_secret ?? "";
      reposRoot = repository.path.split("/").slice(0, -1).join("/") || "/repos";
      tokenMode = "keep";
      secretMode = "keep";
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Unable to load repository.",
      );
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
      const id = page.params.id;
      if (!id) return;
      const payload = buildRepositoryPayload(
        form,
        "edit",
        tokenMode,
        secretMode,
      );
      const repository = await updateRepository(id, payload);
      window.location.assign(`/repositories/${repository.id}`);
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Unable to update repository.";
      toast.error(message);
    } finally {
      saving = false;
    }
  }

  async function copyWebhookSecret(): Promise<void> {
    if (!form.webhook_secret) return;
    await navigator.clipboard.writeText(form.webhook_secret);
    toast.success("Webhook secret copied.");
  }

  function regenerateWebhookSecret(): void {
    form.webhook_secret = generateWebhookSecret();
    secretMode = "replace";
    secretVisible = true;
  }

  function clearWebhookSecret(): void {
    form.webhook_secret = "";
    secretMode = "clear";
  }
</script>

<svelte:head>
  <title>Edit repository · GitSync</title>
</svelte:head>

<section class="page-shell">
  <Heading level={1} size="lg">Edit repository</Heading>
  <form
    class="form-grid"
    onsubmit={(event) => {
      event.preventDefault();
      void submit();
    }}
  >
    <div class="field">
      <label for="name">Name</label>
      <Input
        id="name"
        bind:value={form.name}
        oninput={(event) => {
          handleNameChange((event.currentTarget as HTMLInputElement).value);
        }}
        aria-invalid={Boolean(errors.name)}
      />
      {#if errors.name}<small class="error">{errors.name}</small>{/if}
    </div>

    <div class="field">
      <label for="url">Repository URL</label>
      <Input
        id="url"
        bind:value={form.url}
        aria-invalid={Boolean(errors.url)}
      />
      {#if errors.url}<small class="error">{errors.url}</small>{/if}
    </div>

    <div class="field">
      <label for="branch">Branch</label>
      <Input
        id="branch"
        bind:value={form.branch}
        placeholder="default branch"
        aria-invalid={Boolean(errors.branch)}
      />
      {#if errors.branch}<small class="error">{errors.branch}</small>{/if}
    </div>

    <div class="field">
      <label for="path">Clone path</label>
      <Input
        id="path"
        bind:value={form.path}
        aria-invalid={Boolean(errors.path)}
        onfocus={() => (pathTouched = true)}
      />
      {#if errors.path}<small class="error">{errors.path}</small>{/if}
      <small class="hint">
        Keep an absolute path; existing files are not moved.
      </small>
    </div>

    <div class="field">
      <label for="access_username">Access username</label>
      <Input
        id="access_username"
        bind:value={form.access_username}
        aria-invalid={Boolean(errors.access_username)}
      />
      {#if errors.access_username}<small class="error"
          >{errors.access_username}</small
        >{/if}
    </div>

    <div class="field">
      <label for="access_token">Access token</label>
      <Input
        id="access_token"
        bind:value={form.access_token}
        type="password"
        placeholder="Keep existing token"
      />
      <div class="mode-row">
        <label class="inline-label" for="token-mode">Token mode</label>
        <select id="token-mode" bind:value={tokenMode}>
          <option value="keep">Keep current token</option>
          <option value="replace">Replace token</option>
          <option value="clear">Clear token</option>
        </select>
      </div>
    </div>

    <div class="field field-span-2">
      <label for="post_command">Post command</label>
      <Textarea id="post_command" bind:value={form.post_command} rows={5} />
    </div>

    <div class="field">
      <label for="timeout">Timeout seconds</label>
      <Input
        id="timeout"
        bind:value={form.post_command_timeout_seconds}
        type="number"
        min="0"
        step="1"
      />
      {#if errors.post_command_timeout_seconds}<small class="error"
          >{errors.post_command_timeout_seconds}</small
        >{/if}
    </div>

    <div class="field">
      <label for="hard_reset">Hard reset</label>
      <Toggle
        bind:checked={form.hard_reset}
        label="Discard local changes on sync"
      />
      <small class="warning">
        Warning: hard reset discards local changes before the next sync.
      </small>
    </div>

    <div class="field field-span-2">
      <label for="webhook_secret">Webhook secret</label>
      <div class="secret-row">
        <Input
          id="webhook_secret"
          bind:value={form.webhook_secret}
          type={secretVisible ? "text" : "password"}
          placeholder="Leave blank to keep the current secret"
        />
        <div class="secret-actions">
          <Button type="button" variant="secondary" onclick={copyWebhookSecret}>
            Copy
          </Button>
          <Button
            type="button"
            variant="secondary"
            onclick={regenerateWebhookSecret}
          >
            Regenerate
          </Button>
          <Button
            type="button"
            variant="secondary"
            onclick={clearWebhookSecret}
          >
            Clear
          </Button>
        </div>
      </div>
      <div class="mode-row">
        <label class="inline-label" for="secret-mode">Secret mode</label>
        <select id="secret-mode" bind:value={secretMode}>
          <option value="keep">Keep current secret</option>
          <option value="replace">Replace secret</option>
          <option value="clear">Use global secret</option>
        </select>
      </div>
    </div>

    <div class="actions field-span-2">
      <Button
        variant="secondary"
        type="button"
        onclick={() =>
          (window.location.href = resolve("/repositories/[id]", {
            id: page.params.id ?? "",
          }))}
      >
        Cancel
      </Button>
      <Button type="submit" loading={saving}>Save changes</Button>
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
  .warning,
  .error {
    font-size: 0.75rem;
  }

  .warning {
    color: var(--rw-warning-text, #9a6700);
  }

  .error {
    color: var(--rw-danger-text);
  }

  .mode-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .inline-label {
    font-size: 0.8rem;
    color: var(--rw-on-surface-muted);
  }

  select {
    min-height: 2.5rem;
    border: 1px solid var(--rw-outline);
    border-radius: 0.5rem;
    background: var(--rw-surface);
    color: var(--rw-on-surface);
    padding: 0.5rem 0.75rem;
  }

  .secret-row {
    display: grid;
    gap: 0.6rem;
  }

  .secret-row :global(input) {
    width: 100%;
  }

  .secret-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
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
