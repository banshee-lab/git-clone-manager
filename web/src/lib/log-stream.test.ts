import { describe, expect, it } from "vitest";
import { createLogStream } from "./log-stream.svelte";
import { FakeEventSource } from "../test/FakeEventSource";

describe("activity log stream", () => {
  it("replaces from snapshot, appends only new offsets, and closes at end", () => {
    let source: FakeEventSource | undefined;
    const stream = createLogStream("activity/one", (url) => {
      source = new FakeEventSource(url);
      return source;
    });

    expect(source?.url).toBe("/api/activities/activity%2Fone/stream");
    source?.emit("snapshot", { status: "running", log: "first", offset: 5 });
    source?.emit("log", { chunk: " chunk", offset: 11 });
    source?.emit("log", { chunk: " duplicate", offset: 11 });
    source?.emit("log", { chunk: " older", offset: 9 });

    expect(stream.log).toBe("first chunk");
    expect(stream.offset).toBe(11);
    expect(stream.activityStatus).toBe("running");

    source?.emit("end", { status: "success" });
    expect(stream.activityStatus).toBe("success");
    expect(stream.connectionState).toBe("closed");
    expect(source?.closed).toBe(true);
  });
});
