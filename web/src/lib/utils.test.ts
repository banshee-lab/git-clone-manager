import { describe, expect, it } from "vitest";
import {
  formatDateTime,
  formatDuration,
  relativeTime,
  statusTone,
  stripAnsi,
} from "./utils";

describe("frontend utilities", () => {
  it("formats durations and handles missing or invalid values", () => {
    expect(formatDuration(0)).toBe("0s");
    expect(formatDuration(65_000)).toBe("1m 5s");
    expect(formatDuration(3_661_000)).toBe("1h 1m");
    expect(formatDuration(null)).toBe("—");
    expect(formatDuration(Number.NaN)).toBe("—");
    expect(formatDateTime("not a timestamp")).toBe("—");
  });

  it("formats relative dates against an injectable clock", () => {
    const now = Date.parse("2026-10-18T12:00:00Z");
    expect(relativeTime(new Date(now - 120_000), now)).toBe("2 minutes ago");
    expect(relativeTime(new Date(now + 60_000), now)).toBe("in 1 minute");
    expect(relativeTime("invalid", now)).toBe("—");
  });

  it("strips CSI color and OSC terminal escapes", () => {
    expect(stripAnsi("\u001B[32mready\u001B[0m")).toBe("ready");
    expect(stripAnsi("\u001B]0;console title\u0007plain")).toBe("plain");
  });

  it("maps all activity states to RunWisp status tones", () => {
    expect(statusTone("queued")).toBe("warning");
    expect(statusTone("running")).toBe("primary");
    expect(statusTone("success")).toBe("success");
    expect(statusTone("failed")).toBe("danger");
    expect(statusTone("interrupted")).toBe("danger");
  });
});
