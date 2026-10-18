import type { StatusDotTone } from "@runwisp/ui";
import type { ActivityStatus } from "./api/types";

export function formatDateTime(
  value: string | Date | null | undefined,
): string {
  if (!value) return "—";
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function formatDuration(
  milliseconds: number | null | undefined,
): string {
  if (
    milliseconds === null ||
    milliseconds === undefined ||
    !Number.isFinite(milliseconds)
  ) {
    return "—";
  }
  const seconds = Math.floor(Math.max(0, milliseconds) / 1000);
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const remainingSeconds = seconds % 60;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${remainingSeconds}s`;
  return `${remainingSeconds}s`;
}

export function relativeTime(value: string | Date, now = Date.now()): string {
  const timestamp =
    value instanceof Date ? value.getTime() : new Date(value).getTime();
  if (Number.isNaN(timestamp)) return "—";
  const seconds = Math.round((timestamp - now) / 1000);
  const absolute = Math.abs(seconds);
  const [amount, unit]: [number, Intl.RelativeTimeFormatUnit] =
    absolute < 60
      ? [seconds, "second"]
      : absolute < 3600
        ? [Math.round(seconds / 60), "minute"]
        : absolute < 86400
          ? [Math.round(seconds / 3600), "hour"]
          : absolute < 2592000
            ? [Math.round(seconds / 86400), "day"]
            : absolute < 31536000
              ? [Math.round(seconds / 2592000), "month"]
              : [Math.round(seconds / 31536000), "year"];
  return new Intl.RelativeTimeFormat(undefined, { numeric: "auto" }).format(
    amount,
    unit,
  );
}

const ANSI_SEQUENCE =
  // CSI, OSC, and single-character ANSI escape sequences.
  /\u001B(?:\[[0-?]*[ -/]*[@-~]|\][^\u0007]*(?:\u0007|\u001B\\)|[@-_])/g;

export function stripAnsi(value: string): string {
  return value.replace(ANSI_SEQUENCE, "");
}

export function statusTone(status: ActivityStatus): StatusDotTone {
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
  }
}
