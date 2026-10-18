import { describe, expect, it } from "vitest";
import {
  buildRepositoryPayload,
  suggestClonePath,
  validateRepositoryForm,
} from "./repository-form";

describe("repository form helpers", () => {
  it("builds a default clone path from the repository name and root", () => {
    expect(suggestClonePath("My Repo", "/repos")).toBe("/repos/my-repo");
  });

  it("validates required repository fields and URL rules", () => {
    const errors = validateRepositoryForm({
      name: "",
      url: "ftp://example.test/repo.git",
      path: "repos/source",
      access_username: "",
    });

    expect(errors.name).toBe("Repository name is required.");
    expect(errors.url).toBe(
      "Repository URL must be a valid http or https URL.",
    );
    expect(errors.path).toBe("Clone path must be an absolute filesystem path.");
    expect(errors.access_username).toBe("Access username is required.");
  });

  it("validates dataDir boundaries and overlapping existing repository paths", () => {
    const dataDirErrors = validateRepositoryForm(
      {
        name: "Repo",
        url: "https://example.test/repo.git",
        path: "/data/repos/repo",
        access_username: "git",
      },
      { dataDir: "/data" },
    );
    expect(dataDirErrors.path).toBe(
      "Clone path cannot be inside the storage data_dir.",
    );

    const overlapErrors = validateRepositoryForm(
      {
        name: "Repo",
        url: "https://example.test/repo.git",
        path: "/repos/app/sub",
        access_username: "git",
      },
      { existingPaths: ["/repos/app"] },
    );
    expect(overlapErrors.path).toBe(
      "Clone path overlaps another repository path.",
    );
  });

  it("keeps, replaces or clears access and webhook secrets correctly", () => {
    expect(
      buildRepositoryPayload(
        {
          name: "Repo",
          url: "https://example.test/repo.git",
          branch: "main",
          path: "/repos/repo",
          access_username: "git",
          access_token: "secret-token",
          post_command: "echo ok",
          post_command_timeout_seconds: 90,
          hard_reset: false,
          webhook_secret: "abc123",
        },
        "edit",
        "replace",
        "replace",
      ),
    ).toMatchObject({
      access_token: "secret-token",
      webhook_secret: "abc123",
    });

    expect(
      buildRepositoryPayload(
        {
          name: "Repo",
          url: "https://example.test/repo.git",
          branch: "main",
          path: "/repos/repo",
          access_username: "git",
          access_token: "secret-token",
          post_command: "echo ok",
          hard_reset: false,
        },
        "edit",
        "clear",
        "keep",
      ),
    ).toMatchObject({
      clear_access_token: true,
    });

    expect(
      buildRepositoryPayload(
        {
          name: "Repo",
          url: "https://example.test/repo.git",
          branch: "main",
          path: "/repos/repo",
          access_username: "git",
          post_command: "echo ok",
          hard_reset: false,
        },
        "edit",
        "keep",
        "clear",
      ),
    ).toMatchObject({
      webhook_secret: "",
    });

    expect(
      buildRepositoryPayload(
        {
          name: "Repo",
          url: "https://example.test/repo.git",
          branch: "main",
          path: "/repos/repo",
          access_username: "git",
          access_token: "",
          post_command: "echo ok",
          hard_reset: false,
        },
        "edit",
        "replace",
        "keep",
      ),
    ).not.toHaveProperty("access_token");

    expect(
      buildRepositoryPayload(
        {
          name: "Repo",
          url: "https://example.test/repo.git",
          branch: "main",
          path: "/repos/repo",
          access_username: "git",
          post_command: "echo ok",
          hard_reset: false,
          webhook_secret: "",
        },
        "edit",
        "keep",
        "replace",
      ),
    ).not.toHaveProperty("webhook_secret");
  });
});
