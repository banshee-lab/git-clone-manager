export type ActivityStatus =
  "queued" | "running" | "success" | "failed" | "interrupted";
export type ActivityTrigger = "manual" | "webhook";
export type ActivitySource = "manual" | "github" | "gitlab" | "other";
export type ActivityAction = "clone" | "pull" | "";
export type FailedStage =
  | ""
  | "clone"
  | "fetch"
  | "checkout"
  | "merge"
  | "reset"
  | "post_command"
  | "internal";

export interface SessionUser {
  sub: string;
  email: string;
  name: string;
  role: "admin";
}

export interface ApiMeta {
  version: string;
  base_url: string;
  history_max_items: number;
  repos_root: string;
  global_webhook_secret_configured: boolean;
}

export interface ActivitySummary {
  id: string;
  repository_id: string;
  repository_name: string;
  status: ActivityStatus;
  trigger: ActivityTrigger;
  source: ActivitySource;
  triggered_by: string;
  action: ActivityAction;
  failed_stage: FailedStage;
  summary: string;
  commit_before: string;
  commit_after: string;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface Activity extends ActivitySummary {
  log?: string;
}

export interface Attention {
  repository_id: string;
  repository_name: string;
  activity_id: string;
  status: "failed" | "interrupted";
  failed_stage: FailedStage;
  summary: string;
  finished_at: string;
}

export interface Dashboard {
  repository_count: number;
  running: ActivitySummary[];
  recent: ActivitySummary[];
  attention: Attention[];
}

export interface Repository {
  id: string;
  name: string;
  url: string;
  branch: string;
  resolved_branch: string;
  path: string;
  access_username: string;
  has_access_token: boolean;
  post_command: string;
  post_command_timeout_seconds: number;
  hard_reset: boolean;
  webhook_secret: string;
  webhook_url: string;
  created_at: string;
  updated_at: string;
  last_activity?: ActivitySummary | null;
  running_activity_id?: string;
}

export interface RepositoryInput {
  name: string;
  url: string;
  branch: string;
  path: string;
  access_username: string;
  access_token?: string | null;
  clear_access_token?: boolean;
  post_command: string;
  post_command_timeout_seconds: number;
  hard_reset: boolean;
  webhook_secret?: string;
}

export interface SyncAllResult {
  started: ActivitySummary[];
  skipped: Array<{ repository_id: string; reason: string }>;
}

export interface HealthResponse {
  status: "ok";
}

export type WebhookResponse =
  | { status: "accepted"; activity_id: string }
  | { status: "ignored"; reason: string };

export interface ApiErrorEnvelope {
  error: {
    code: string;
    message: string;
  };
}
