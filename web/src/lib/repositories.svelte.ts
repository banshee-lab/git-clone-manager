import { events } from "./events.svelte";
import { listRepositories } from "./api/client";
import type { ActivityStatus, Repository } from "./api/types";
import type { StatusDotTone } from "@runwisp/ui";

export function getRepositoryStatus(
  repository: Pick<Repository, "last_activity" | "running_activity_id">,
): ActivityStatus | "never" {
  if (repository.running_activity_id) return "running";
  if (repository.last_activity) return repository.last_activity.status;
  return "never";
}

export function repositoryStatusTone(
  repository: Pick<Repository, "last_activity" | "running_activity_id">,
): StatusDotTone {
  const status = getRepositoryStatus(repository);
  switch (status) {
    case "success":
      return "success";
    case "failed":
    case "interrupted":
      return "danger";
    case "running":
      return "primary";
    case "queued":
      return "warning";
    case "never":
    default:
      return "muted";
  }
}

export function createRepositoryStore() {
  let repositories = $state<Repository[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let selectedId = $state<string | null>(null);

  async function refresh(options: { silent?: boolean } = {}): Promise<void> {
    if (!options.silent) loading = true;
    try {
      const next = await listRepositories();
      repositories = next;
      if (selectedId && !next.some((repo) => repo.id === selectedId)) {
        selectedId = null;
      }
      error = null;
    } catch (caught) {
      error =
        caught instanceof Error
          ? caught.message
          : "Unable to load repositories.";
    } finally {
      if (!options.silent) loading = false;
    }
  }

  function refreshFromEvent(): void {
    void refresh({ silent: true });
  }

  for (const type of [
    "resync",
    "repository.created",
    "repository.updated",
    "repository.deleted",
    "activity.queued",
    "activity.started",
    "activity.finished",
  ]) {
    events.subscribe(type, refreshFromEvent);
  }

  return {
    get repositories(): Repository[] {
      return repositories;
    },
    get loading(): boolean {
      return loading;
    },
    get error(): string | null {
      return error;
    },
    get selectedId(): string | null {
      return selectedId;
    },
    refresh,
    select(id: string | null): void {
      selectedId = id;
    },
    clearSelection(): void {
      selectedId = null;
    },
  };
}

export const repositories = createRepositoryStore();
