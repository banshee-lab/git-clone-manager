<script lang="ts">
  import "../../edit.css";
  import { onMount } from "svelte";
  import { Button, Heading, Input, Textarea, Toggle, toast } from "@runwisp/ui";
  import { page } from "$app/state";
  import { resolve } from "$app/paths";
  import {
    getMeta,
    getRepository,
    listRepositories,
    updateRepository,
  } from "#lib/api/client";
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
  let dataDir = $state("");
  let existingPaths = $state<string[]>([]);
  let originalPath = $state("");
  let hasToken = $state(false);
  let pathTouched = $state(false);
  let saving = $state(false);
  let tokenMode = $state<TokenMode>("keep");
  let secretMode = $state<SecretMode>("keep");
  let secretVisible = $state(false);

  onMount(async () => {
    const id = page.params.id;
    if (!id) return;
    try {
      const meta = await getMeta();
      reposRoot = meta.repos_root || "/repos";
      dataDir = meta.data_dir || "";
    } catch {
      reposRoot = "/repos";
      dataDir = "";
    }
    try {
      const all = await listRepositories();
      existingPaths = all.filter((r) => r.id !== id).map((r) => r.path);
    } catch {
      existingPaths = [];
    }
    try {
      const repository = await getRepository(id);
      form.name = repository.name;
      form.url = repository.url;
      form.branch = repository.branch;
      form.path = repository.path;
      originalPath = repository.path;
      hasToken = repository.has_access_token;
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
    const nextErrors = validateRepositoryForm(form, {
      dataDir,
      existingPaths,
    });
    if (tokenMode === "replace" && !form.access_token.trim()) {
      nextErrors.access_token =
        "A new access token is required when replacing.";
    }
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
      const lower = message.toLowerCase();
      if (
        lower.includes("path") ||
        lower.includes("data_dir") ||
        lower.includes("overlap")
      ) {
        errors = { ...errors, path: message };
      }
      if (lower.includes("name")) {
        errors = { ...errors, name: message };
      }
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

<section class="repository-edit-page page-shell">
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
      {#if originalPath && form.path !== originalPath}
        <small class="warning"> Warning: existing files are not moved. </small>
      {/if}
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
      <div class="label-row">
        <label for="access_token">Access token</label>
        {#if hasToken}
          <span class="token-badge">Token set</span>
        {/if}
      </div>
      <Input
        id="access_token"
        bind:value={form.access_token}
        type="password"
        placeholder={tokenMode === "keep"
          ? "Keep existing token"
          : tokenMode === "clear"
            ? "Token will be cleared"
            : "Enter new token"}
        disabled={tokenMode === "clear"}
        aria-invalid={Boolean(errors.access_token)}
      />
      {#if errors.access_token}<small class="error">{errors.access_token}</small
        >{/if}
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
