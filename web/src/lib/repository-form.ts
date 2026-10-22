import type { RepositoryInput } from "./api/types";

export type TokenMode = "keep" | "replace" | "clear";
export type SecretMode = "keep" | "replace" | "clear";

export function normalizeFilesystemPath(value: string): string {
  const trimmed = (value ?? "").trim();
  if (!trimmed) return "";
  const absolute = trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
  const segments: string[] = [];
  for (const segment of absolute.split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") {
      if (segments.length > 0) segments.pop();
      continue;
    }
    segments.push(segment);
  }
  const normalized = `/${segments.join("/")}`;
  return normalized === "" ? "/" : normalized;
}

export function suggestClonePath(name: string, reposRoot = "/repos"): string {
  const slug = (name ?? "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
  const base = (reposRoot ?? "/repos").replace(/\/+$/, "") || "/repos";
  return `${base}/${slug || "repository"}`;
}

export function generateWebhookSecret(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

export function validateRepositoryForm(
  input: Partial<RepositoryInput> & {
    name?: string;
    url?: string;
    branch?: string;
    path?: string;
    access_username?: string;
  },
  options: {
    dataDir?: string;
    existingPaths?: string[];
  } = {},
): Record<string, string> {
  const errors: Record<string, string> = {};
  const name = (input.name ?? "").trim();
  const url = (input.url ?? "").trim();
  const branch = (input.branch ?? "").trim();
  const path = (input.path ?? "").trim();
  const accessUsername = (input.access_username ?? "").trim();

  if (!name) {
    errors.name = "Repository name is required.";
  } else if (name.length > 100) {
    errors.name = "Repository name must be 1-100 characters.";
  }

  if (!url) {
    errors.url = "Repository URL is required.";
  } else {
    try {
      const parsed = new URL(url);
      if (!["http:", "https:"].includes(parsed.protocol) || !parsed.hostname) {
        errors.url = "Repository URL must be a valid http or https URL.";
      }
    } catch {
      errors.url = "Repository URL must be a valid http or https URL.";
    }
  }

  if (!branch) {
    // branch is optional; empty value is acceptable and uses default logic
  } else if (
    /\s/.test(branch) ||
    branch.includes("..") ||
    branch.startsWith("-")
  ) {
    errors.branch = "Branch contains invalid characters.";
  }

  if (!path) {
    errors.path = "Clone path is required.";
  } else {
    if (!path.startsWith("/")) {
      errors.path = "Clone path must be an absolute filesystem path.";
    } else if (path !== "/" && normalizeFilesystemPath(path) !== path) {
      errors.path = "Clone path must be normalized and absolute.";
    } else if (path === "/") {
      errors.path = "Clone path must be an absolute clone path.";
    } else if (options.dataDir) {
      const dataDir = (options.dataDir ?? "").trim();
      if (dataDir && isInsideDir(path, dataDir)) {
        errors.path = `Clone path cannot be inside the storage data directory (${dataDir}).`;
      }
    }
    if (!errors.path && options.existingPaths?.length) {
      for (const existing of options.existingPaths) {
        if (!existing || existing === path) continue;
        if (isPathOverlap(path, existing)) {
          errors.path = "Clone path overlaps another repository path.";
          break;
        }
      }
    }
  }

  if (!accessUsername) {
    errors.access_username = "Access username is required.";
  }

  if (
    input.post_command_timeout_seconds !== undefined &&
    input.post_command_timeout_seconds !== null &&
    Number.isNaN(Number(input.post_command_timeout_seconds))
  ) {
    errors.post_command_timeout_seconds = "Timeout must be a number.";
  }

  if (
    input.post_command_timeout_seconds !== undefined &&
    input.post_command_timeout_seconds !== null &&
    Number(input.post_command_timeout_seconds) < 0
  ) {
    errors.post_command_timeout_seconds =
      "Timeout must be greater than or equal to zero.";
  }

  return errors;
}

function isInsideDir(path: string, dir: string): boolean {
  const base = normalizeFilesystemPath((dir ?? "").trim());
  const candidate = normalizeFilesystemPath((path ?? "").trim());
  if (!candidate || !base) return false;
  if (candidate === base) return true;
  return base === "/"
    ? candidate.startsWith("/")
    : candidate.startsWith(`${base}/`);
}

function isPathOverlap(a: string, b: string): boolean {
  const left = a.trim();
  const right = b.trim();
  if (!left || !right || left === right) return left === right;
  return left.startsWith(`${right}/`) || right.startsWith(`${left}/`);
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
    if (input.access_token && input.access_token.trim() !== "") {
      payload.access_token = input.access_token;
    }
    if (
      secretMode === "replace" &&
      input.webhook_secret &&
      input.webhook_secret.trim() !== ""
    ) {
      payload.webhook_secret = input.webhook_secret;
    }
    if (secretMode === "clear") {
      payload.webhook_secret = "";
    }
    return payload;
  }

  if (
    tokenMode === "replace" &&
    input.access_token !== undefined &&
    input.access_token !== ""
  ) {
    payload.access_token = input.access_token;
  }
  if (tokenMode === "clear") {
    payload.clear_access_token = true;
  }
  if (secretMode === "clear") {
    payload.webhook_secret = "";
  }
  if (
    secretMode === "replace" &&
    input.webhook_secret !== undefined &&
    input.webhook_secret !== ""
  ) {
    payload.webhook_secret = input.webhook_secret;
  }

  return payload;
}
