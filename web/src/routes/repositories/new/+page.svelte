<script lang="ts">
  import "../new.css";
  import { onMount } from "svelte";
  import { Button, Heading, Input, Textarea, Toggle } from "@runwisp/ui";
  import { createRepository, getMeta, listRepositories } from "#lib/api/client";
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
  let dataDir = $state("");
  let existingPaths = $state<string[]>([]);
  let pathTouched = $state(false);
  let saving = $state(false);

  onMount(async () => {
    try {
      const meta = await getMeta();
      reposRoot = meta.repos_root || "/repos";
      dataDir = meta.data_dir || "";
      if (!pathTouched && form.path === "") {
        form.path = suggestClonePath(form.name, reposRoot);
      }
    } catch {
      reposRoot = "/repos";
    }
    try {
      const repos = await listRepositories();
      existingPaths = repos.map((r) => r.path);
    } catch {
      existingPaths = [];
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
</script>

<svelte:head>
  <title>Create repository · Git Clone Manager</title>
</svelte:head>

<section class="repository-new-page page-shell">
  <Heading level={1} size="lg">Add repository</Heading>
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
        error={errors.name}
      />
    </div>

    <div class="field">
      <label for="url">Repository URL</label>
      <Input
        id="url"
        bind:value={form.url}
        error={errors.url}
      />
    </div>

    <div class="field">
      <label for="branch">Branch</label>
      <Input
        id="branch"
        bind:value={form.branch}
        placeholder="default branch"
        error={errors.branch}
      />
    </div>

    <div class="field">
      <label for="path">Clone path</label>
      <Input
        id="path"
        bind:value={form.path}
        error={errors.path}
        onfocus={() => (pathTouched = true)}
      />
      <small class="hint">
        Suggested default: {suggestClonePath(
          form.name || "repository",
          reposRoot,
        )}
      </small>
    </div>

    <div class="field">
      <label for="access_username">Access username</label>
      <Input
        id="access_username"
        bind:value={form.access_username}
        error={errors.access_username}
      />
    </div>

    <div class="field">
      <label for="access_token">Access token</label>
      <Input
        id="access_token"
        bind:value={form.access_token}
        type="password"
        placeholder="Optional"
      />
    </div>

    <div class="field field-span-2">
      <label for="post_command">Post command</label>
      <Textarea
        id="post_command"
        bind:value={form.post_command}
        rows={5}
        placeholder="Optional shell command to run after sync"
      />
    </div>

    <div class="field">
      <label for="timeout">Timeout seconds</label>
      <Input
        id="timeout"
        bind:value={form.post_command_timeout_seconds}
        type="number"
        min="0"
        step="1"
        error={errors.post_command_timeout_seconds}
      />
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
      <Input
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
      >
      <Button type="submit" loading={saving}>Create repository</Button>
    </div>
  </form>
</section>
