import { mockApiResponse } from "./mock";
import type {
  Activity,
  ActivitySummary,
  ApiErrorEnvelope,
  ApiMeta,
  Dashboard,
  HealthResponse,
  Repository,
  RepositoryInput,
  SessionUser,
  SyncAllResult,
  WebhookResponse,
} from "./types";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export interface RequestContext {
  fetcher?: typeof fetch;
  next?: string;
  location?: Pick<Location, "assign">;
  skipUnauthorizedRedirect?: boolean;
}

function currentPath(): string {
  if (typeof window === "undefined") return "/";
  return `${window.location.pathname}${window.location.search}`;
}

function errorEnvelope(value: unknown): ApiErrorEnvelope | undefined {
  if (!value || typeof value !== "object" || !("error" in value))
    return undefined;
  const { error } = value;
  if (
    !error ||
    typeof error !== "object" ||
    !("code" in error) ||
    !("message" in error) ||
    typeof error.code !== "string" ||
    typeof error.message !== "string"
  ) {
    return undefined;
  }
  return { error: { code: error.code, message: error.message } };
}

async function send(
  path: string,
  init: RequestInit,
  context: RequestContext,
): Promise<Response> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response =
    import.meta.env.VITE_MOCK_API === "true"
      ? mockApiResponse(path, { ...init, headers })
      : await (context.fetcher ?? fetch)(path, { ...init, headers });

  if (!response.ok) {
    const payload: unknown = await response.json().catch(() => undefined);
    const envelope = errorEnvelope(payload);
    if (response.status === 401 && !context.skipUnauthorizedRedirect) {
      const next = context.next ?? currentPath();
      const target = `/auth/login?next=${encodeURIComponent(next)}`;
      const location =
        context.location ??
        (typeof window !== "undefined" ? window.location : undefined);
      location?.assign(target);
    }
    throw new ApiError(
      response.status,
      envelope?.error.code ?? "internal",
      envelope?.error.message ?? response.statusText ?? "Request failed",
    );
  }

  return response;
}

export async function request<T>(
  path: string,
  init: RequestInit = {},
  context: RequestContext = {},
): Promise<T> {
  const response = await send(path, init, context);
  if (response.status === 204) return undefined as T;

  try {
    return (await response.json()) as T;
  } catch {
    throw new ApiError(500, "internal", "The server returned invalid JSON.");
  }
}

function withQuery(
  path: string,
  values: Record<string, string | number | boolean | undefined>,
): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined) query.set(key, String(value));
  }
  const search = query.toString();
  return search ? `${path}?${search}` : path;
}

export function loginUrl(next = "/"): string {
  return `/auth/login?next=${encodeURIComponent(next)}`;
}

export function startLogin(next = "/"): void {
  if (typeof window !== "undefined") window.location.assign(loginUrl(next));
}

export function getHealth(context?: RequestContext): Promise<HealthResponse> {
  return request("/healthz", {}, context);
}

export function getMe(context?: RequestContext): Promise<SessionUser> {
  return request("/api/me", {}, context);
}

export function getMeta(context?: RequestContext): Promise<ApiMeta> {
  return request("/api/meta", {}, context);
}

export function getDashboard(context?: RequestContext): Promise<Dashboard> {
  return request("/api/dashboard", {}, context);
}

export function listRepositories(
  context?: RequestContext,
): Promise<Repository[]> {
  return request("/api/repositories", {}, context);
}

export function createRepository(
  input: RepositoryInput,
  context?: RequestContext,
): Promise<Repository> {
  return request(
    "/api/repositories",
    { method: "POST", body: JSON.stringify(input) },
    context,
  );
}

export function getRepository(
  id: string,
  context?: RequestContext,
): Promise<Repository> {
  return request(`/api/repositories/${encodeURIComponent(id)}`, {}, context);
}

export function updateRepository(
  id: string,
  input: RepositoryInput,
  context?: RequestContext,
): Promise<Repository> {
  return request(
    `/api/repositories/${encodeURIComponent(id)}`,
    {
      method: "PUT",
      body: JSON.stringify(input),
    },
    context,
  );
}

export function deleteRepository(
  id: string,
  deleteFiles = false,
  context?: RequestContext,
): Promise<void> {
  const path = withQuery(`/api/repositories/${encodeURIComponent(id)}`, {
    delete_files: deleteFiles ? true : undefined,
  });
  return request(path, { method: "DELETE" }, context);
}

export function syncRepository(
  id: string,
  context?: RequestContext,
): Promise<ActivitySummary> {
  return request(
    `/api/repositories/${encodeURIComponent(id)}/sync`,
    { method: "POST" },
    context,
  );
}

export function syncAll(context?: RequestContext): Promise<SyncAllResult> {
  return request("/api/sync", { method: "POST" }, context);
}

export function listActivities(
  repositoryId: string,
  options: { limit?: number; before?: string } = {},
  context?: RequestContext,
): Promise<ActivitySummary[]> {
  const path = withQuery(
    `/api/repositories/${encodeURIComponent(repositoryId)}/activities`,
    options,
  );
  return request(path, {}, context);
}

export function getActivity(
  id: string,
  context?: RequestContext,
): Promise<Activity> {
  return request(`/api/activities/${encodeURIComponent(id)}`, {}, context);
}

export async function getActivityLog(
  id: string,
  download = false,
  context: RequestContext = {},
): Promise<string> {
  const path = withQuery(`/api/activities/${encodeURIComponent(id)}/log`, {
    download: download ? 1 : undefined,
  });
  const response = await send(path, {}, context);
  return response.text();
}

export function logout(context?: RequestContext): Promise<void> {
  return request("/auth/logout", { method: "POST" }, context);
}

export function sendWebhook(
  repositoryId: string,
  body: BodyInit,
  headers: HeadersInit,
  context: RequestContext = {},
): Promise<WebhookResponse> {
  return request(
    `/hooks/${encodeURIComponent(repositoryId)}`,
    { method: "POST", body, headers },
    { ...context, skipUnauthorizedRedirect: true },
  );
}

export type {
  Activity,
  ActivitySummary,
  ApiMeta,
  Dashboard,
  Repository,
  RepositoryInput,
};
