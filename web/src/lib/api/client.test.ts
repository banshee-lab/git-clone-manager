import { describe, expect, it, vi } from "vitest";
import { ApiError, createRepository, listActivities, request } from "./client";
import type { RepositoryInput } from "./types";

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("API client", () => {
  it("maps the API error envelope into ApiError", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        {
          error: {
            code: "conflict",
            message: "Repository is already syncing.",
          },
        },
        409,
      ),
    );

    await expect(
      request("/api/test", {}, { fetcher: fetchMock as typeof fetch }),
    ).rejects.toEqual(
      expect.objectContaining<ApiError>({
        name: "ApiError",
        status: 409,
        code: "conflict",
        message: "Repository is already syncing.",
      }),
    );
  });

  it("redirects unauthorized requests to login with the current path encoded", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        { error: { code: "unauthorized", message: "Sign in required." } },
        401,
      ),
    );
    const location = { assign: vi.fn() };

    await expect(
      request(
        "/api/me",
        {},
        {
          fetcher: fetchMock as typeof fetch,
          next: "/repositories/a?filter=two words",
          location,
        },
      ),
    ).rejects.toBeInstanceOf(ApiError);

    expect(location.assign).toHaveBeenCalledWith(
      "/auth/login?next=%2Frepositories%2Fa%3Ffilter%3Dtwo%20words",
    );
  });

  it("sends JSON request bodies and builds encoded query parameters", async () => {
    const fetchMock = vi
      .fn(async (_input: RequestInfo | URL, _init?: RequestInit) =>
        jsonResponse({}),
      )
      .mockResolvedValueOnce(jsonResponse({ id: "repo-1" }))
      .mockResolvedValueOnce(jsonResponse([]));
    const context = { fetcher: fetchMock as typeof fetch };
    const input: RepositoryInput = {
      name: "Source",
      url: "https://example.test/source.git",
      branch: "main",
      path: "/repos/source",
      access_username: "git",
      post_command: "",
      post_command_timeout_seconds: 0,
      hard_reset: false,
    };

    await createRepository(input, context);
    await listActivities(
      "repo/id",
      { limit: 25, before: "2026-10-18T10:30:00Z" },
      context,
    );

    const firstCall = fetchMock.mock.calls[0];
    expect(firstCall?.[0]).toBe("/api/repositories");
    expect(firstCall?.[1]?.method).toBe("POST");
    expect(firstCall?.[1]?.body).toBe(JSON.stringify(input));
    expect(new Headers(firstCall?.[1]?.headers).get("Content-Type")).toBe(
      "application/json",
    );

    const secondCall = fetchMock.mock.calls[1];
    const url = new URL(String(secondCall?.[0]), "http://localhost");
    expect(url.pathname).toBe("/api/repositories/repo%2Fid/activities");
    expect(url.searchParams.get("limit")).toBe("25");
    expect(url.searchParams.get("before")).toBe("2026-10-18T10:30:00Z");
  });
});
