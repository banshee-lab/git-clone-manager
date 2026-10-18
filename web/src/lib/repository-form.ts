import type { RepositoryInput } from "./api/types";

export type TokenMode = "keep" | "replace" | "clear";
export type SecretMode = "keep" | "replace" | "clear";

export function suggestClonePath(name: string, reposRoot = "/repos"): string {
  const slug = (name ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  const base = (reposRoot ?? "/repos").replace(/\/+$/, "") || "/repos";
  return `${base}/${slug || "repository"}`;
}

export function validateRepositoryForm(
  input: Partial<RepositoryInput> & {
    name?: string;
    url?: string;
    branch?: string;
    path?: string;
    access_username?: string;
  },
): Record<string, string> {
  const errors: Record<string, string> = {};
  const name = (input.name ?? "").trim();
  const url = (input.url ?? "").trim();
  const branch = (input.branch ?? "").trim();
  const path = (input.path ?? "").trim();
  const accessUsername = (input.access_username ?? "").trim();

  if (!name) errors.name = "Repository name is required.";
  if (!url) errors.url = "Repository URL is required.";
  else if (!/^https?:\/\//i.test(url))
    errors.url = "Repository URL must begin with http:// or https://.";
  if (!path) errors.path = "Clone path is required.";
  else if (!path.startsWith("/"))
    errors.path = "Clone path must be an absolute filesystem path.";
  if (!accessUsername) errors.access_username = "Access username is required.";
  if (
    input.post_command_timeout_seconds !== undefined &&
    Number.isNaN(Number(input.post_command_timeout_seconds))
  ) {
    errors.post_command_timeout_seconds = "Timeout must be a number.";
  }
  if (branch && branch.includes(" ")) {
    errors.branch = "Branch names cannot contain spaces.";
  }

  return errors;
}

export function buildRepositoryPayload(
  input: Partial<RepositoryInput> & {
    name: string;
    url: string;
    branch: string;
    path: string;
    access_username: string;
    post_command?: string;
    post_command_timeout_seconds?: number | string;
    hard_reset?: boolean;
    webhook_secret?: string;
  },
  mode: "create" | "edit" = "create",
  tokenMode: TokenMode = "keep",
  secretMode: SecretMode = "keep",
): RepositoryInput & { clear_access_token?: boolean } {
  const payload: RepositoryInput & { clear_access_token?: boolean } = {
    name: (input.name ?? "").trim(),
    url: (input.url ?? "").trim(),
    branch: (input.branch ?? "").trim(),
    path: (input.path ?? "").trim(),
    access_username: (input.access_username ?? "git").trim() || "git",
    post_command: input.post_command ?? "",
    post_command_timeout_seconds: Number(
      input.post_command_timeout_seconds ?? 0,
    ),
    hard_reset: Boolean(input.hard_reset),
  };

  if (mode === "create") {
    if (input.access_token) {
      payload.access_token = input.access_token;
    }
    if (secretMode === "replace" && input.webhook_secret) {
      payload.webhook_secret = input.webhook_secret;
    }
    if (secretMode === "clear") {
      payload.webhook_secret = "";
    }
    return payload;
  }

  if (tokenMode === "replace" && input.access_token !== undefined) {
    payload.access_token = input.access_token;
  }
  if (tokenMode === "clear") {
    payload.clear_access_token = true;
  }
  if (secretMode === "clear") {
    payload.webhook_secret = "";
  }
  if (secretMode === "replace" && input.webhook_secret !== undefined) {
    payload.webhook_secret = input.webhook_secret;
  }

  return payload;
}
