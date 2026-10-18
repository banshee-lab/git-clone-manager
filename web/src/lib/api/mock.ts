import type { ApiMeta, Dashboard, SessionUser } from "./types";

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

const mockDashboard: Dashboard = {
  repository_count: 0,
  running: [],
  recent: [],
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
    payload = [];
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
