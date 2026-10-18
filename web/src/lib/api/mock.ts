import type {
  Activity,
  ApiMeta,
  Dashboard,
  Repository,
  SessionUser,
} from "./types";

const mockUser: SessionUser = {
  sub: "mock-admin",
  email: "admin@example.test",
  name: "GitSync admin",
  role: "admin",
};

const mockMeta: ApiMeta = {
  version: "development",
  base_url: "http://localhost:8080",
  history_max_items: 50,
  repos_root: "/repos",
  global_webhook_secret_configured: false,
};

const mockRepositories: Repository[] = [
  {
    id: "repo-1",
    name: "CLI tools",
    url: "https://github.com/example/cli-tools.git",
    branch: "main",
    resolved_branch: "main",
    path: "/repos/cli-tools",
    access_username: "git",
    has_access_token: false,
    post_command: "echo done",
    post_command_timeout_seconds: 120,
    hard_reset: false,
    webhook_secret: "demo-secret",
    webhook_url: "http://localhost:8080/hooks/repo-1",
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-02T00:00:00Z",
    last_activity: {
      id: "activity-1",
      repository_id: "repo-1",
      repository_name: "CLI tools",
      status: "success",
      trigger: "manual",
      source: "github",
      triggered_by: "admin@example.test",
      action: "pull",
      failed_stage: "",
      summary: "Repository synced successfully.",
      commit_before: "abc1234",
      commit_after: "def5678",
      created_at: "2024-01-02T00:00:00Z",
      started_at: "2024-01-02T00:00:00Z",
      finished_at: "2024-01-02T00:01:00Z",
    },
    running_activity_id: undefined,
  },
  {
    id: "repo-2",
    name: "Web app",
    url: "https://gitlab.example.com/team/web-app.git",
    branch: "develop",
    resolved_branch: "develop",
    path: "/repos/web-app",
    access_username: "git",
    has_access_token: true,
    post_command: "npm run build",
    post_command_timeout_seconds: 300,
    hard_reset: true,
    webhook_secret: "global-secret",
    webhook_url: "http://localhost:8080/hooks/repo-2",
    created_at: "2024-01-03T00:00:00Z",
    updated_at: "2024-01-04T00:00:00Z",
    last_activity: {
      id: "activity-2",
      repository_id: "repo-2",
      repository_name: "Web app",
      status: "running",
      trigger: "manual",
      source: "manual",
      triggered_by: "admin@example.test",
      action: "pull",
      failed_stage: "",
      summary: "Sync in progress.",
      commit_before: "fedcba9",
      commit_after: "",
      created_at: "2024-01-04T00:00:00Z",
      started_at: "2024-01-04T00:00:00Z",
      finished_at: null,
    },
    running_activity_id: "activity-2",
  },
];

const mockDashboard: Dashboard = {
  repository_count: 2,
  running: mockRepositories
    .filter((repo) => repo.running_activity_id)
    .map((repo) => repo.last_activity as Activity),
  recent: mockRepositories.flatMap((repo) =>
    repo.last_activity ? [repo.last_activity] : [],
  ),
  attention: [],
};

export function mockApiResponse(
  path: string,
  init: RequestInit = {},
): Response {
  const method = (init.method ?? "GET").toUpperCase();
  let payload: unknown;
  let status = 200;

  if (path === "/api/me" && method === "GET") {
    payload = mockUser;
  } else if (path === "/api/meta" && method === "GET") {
    payload = mockMeta;
  } else if (path === "/api/dashboard" && method === "GET") {
    payload = mockDashboard;
  } else if (path === "/api/repositories" && method === "GET") {
    payload = mockRepositories;
  } else if (path.startsWith("/api/repositories/") && method === "GET") {
    const id = decodeURIComponent(path.split("/").slice(3).join("/"));
    const repo = mockRepositories.find((entry) => entry.id === id);
    payload = repo ?? {
      error: { code: "not_found", message: "Repository not found." },
    };
    if (!repo) status = 404;
  } else if (path === "/auth/logout" && method === "POST") {
    return new Response(null, { status: 204 });
  } else if (path === "/healthz" && method === "GET") {
    payload = { status: "ok" };
  } else {
    status = 404;
    payload = {
      error: {
        code: "not_found",
        message: "No development mock exists for this route.",
      },
    };
  }

  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8" },
  });
}
