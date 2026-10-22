import { cleanup, render, screen, waitFor } from "@testing-library/svelte";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import DashboardPage from "./+page.svelte";
import { getDashboard, syncRepository } from "#lib/api/client";
import type { ActivitySummary, Dashboard } from "#lib/api/types";
import { FakeEventSource } from "../test/FakeEventSource";
import { toast } from "@runwisp/ui";
import { ApiError } from "#lib/api/client";

vi.mock("#lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("#lib/api/client")>();
  return {
    ...actual,
    getDashboard: vi.fn(),
    syncRepository: vi.fn(),
  };
});

function activity(overrides: Partial<ActivitySummary> = {}): ActivitySummary {
  return {
    id: "activity-1",
    repository_id: "repo-1",
    repository_name: "CLI tools",
    status: "success",
    trigger: "manual",
    source: "manual",
    triggered_by: "admin@example.test",
    action: "pull",
    failed_stage: "",
    summary: "Synced successfully",
    commit_before: "a1b2c3d",
    commit_after: "d4e5f6a",
    created_at: "2026-10-21T12:00:00Z",
    started_at: "2026-10-21T12:00:01Z",
    finished_at: "2026-10-21T12:00:10Z",
    ...overrides,
  };
}

function dashboard(overrides: Partial<Dashboard> = {}): Dashboard {
  return {
    repository_count: 1,
    running: [],
    recent: [activity()],
    attention: [],
    ...overrides,
  };
}

describe("dashboard page", () => {
  beforeEach(() => {
    vi.mocked(getDashboard).mockResolvedValue(dashboard());
    vi.mocked(syncRepository).mockResolvedValue(
      activity({ status: "queued", finished_at: null }),
    );
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("shows the first-run call to action when no repositories exist", async () => {
    vi.mocked(getDashboard).mockResolvedValue(
      dashboard({ repository_count: 0, recent: [] }),
    );
    render(DashboardPage);

    expect(await screen.findByTestId("first-run-cta")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute(
      "href",
      "/repositories",
    );
  });

  it("renders live, attention, recent panels and retries a failed repository", async () => {
    vi.mocked(getDashboard).mockResolvedValue(
      dashboard({
        running: [
          activity({ id: "running-1", status: "running", finished_at: null }),
        ],
        attention: [
          {
            repository_id: "repo-1",
            repository_name: "CLI tools",
            activity_id: "failed-1",
            status: "failed",
            failed_stage: "merge",
            summary: "Merge conflict",
            finished_at: "2026-10-21T11:00:00Z",
          },
        ],
      }),
    );
    const user = userEvent.setup();
    render(DashboardPage);

    expect(await screen.findByTestId("stat-running")).toHaveTextContent("1");
    expect(screen.getByTestId("stat-attention")).toHaveTextContent("1");
    expect(screen.getByText("Merge conflict")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /CLI tools.*Manual.*running/ }),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(syncRepository).toHaveBeenCalledWith("repo-1"));
    expect(
      document.querySelector<HTMLAnchorElement>('a[href*="activity=failed-1"]'),
    ).toBeInTheDocument();
  });

  it("reports a 409 retry conflict and clears its busy state", async () => {
    vi.mocked(getDashboard).mockResolvedValue(
      dashboard({
        attention: [
          {
            repository_id: "repo-1",
            repository_name: "CLI tools",
            activity_id: "failed-1",
            status: "failed",
            failed_stage: "fetch",
            summary: "Remote unavailable",
            finished_at: "2026-10-21T11:00:00Z",
          },
        ],
      }),
    );
    vi.mocked(syncRepository).mockRejectedValue(
      new ApiError(409, "conflict", "Sync already queued."),
    );
    const errorToast = vi.spyOn(toast, "error");
    const user = userEvent.setup();
    render(DashboardPage);

    const retry = await screen.findByRole("button", { name: "Retry" });
    await user.click(retry);
    await waitFor(() => expect(syncRepository).toHaveBeenCalledWith("repo-1"));
    await waitFor(() => expect(retry).not.toBeDisabled());
    expect(errorToast).toHaveBeenCalledWith("Sync already queued.");
  });

  it("streams snapshot, chunks and end, keeps the completed console open, and restores focus", async () => {
    const current = activity({
      id: "running-1",
      status: "running",
      finished_at: null,
    });
    vi.mocked(getDashboard).mockResolvedValue(
      dashboard({ running: [current] }),
    );
    const user = userEvent.setup();
    render(DashboardPage);

    const activityButton = await screen.findByRole("button", {
      name: /CLI tools.*Manual.*running/,
    });
    await user.click(activityButton);
    expect(
      await screen.findByRole("dialog", { name: "Live console" }),
    ).toBeInTheDocument();
    const stream = FakeEventSource.instances.at(-1);
    expect(stream?.url).toContain("/api/activities/running-1/stream");
    stream?.emit("snapshot", {
      status: "running",
      log: "starting\n",
      offset: 9,
    });
    stream?.emit("log", { chunk: "done\n", offset: 14 });
    await waitFor(() =>
      expect(
        document.querySelector(".console-log-shell")?.textContent,
      ).toContain("done"),
    );
    stream?.emit("end", { status: "success" });

    expect(await screen.findByText("success")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Open repository" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("dialog", { name: "Live console" }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Close console" }));
    await waitFor(() => expect(document.activeElement).toBe(activityButton));
  });

  it("announces empty running and attention states", async () => {
    vi.mocked(getDashboard).mockResolvedValue(dashboard({ recent: [] }));
    render(DashboardPage);

    expect(await screen.findByText("Nothing running.")).toBeInTheDocument();
    expect(
      screen.getByText("All good — nothing needs attention."),
    ).toBeInTheDocument();
    expect(screen.getByText("No recent activity.")).toBeInTheDocument();
  });
});
