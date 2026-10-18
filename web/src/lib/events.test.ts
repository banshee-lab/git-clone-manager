import { describe, expect, it, vi } from "vitest";
import { createEventsStore } from "./events.svelte";
import { FakeEventSource } from "../test/FakeEventSource";

describe("global event stream", () => {
  it("dispatches named events and emits resync each time the connection opens", () => {
    let source: FakeEventSource | undefined;
    const store = createEventsStore((url) => {
      source = new FakeEventSource(url);
      return source;
    });
    const finished = vi.fn();
    const resync = vi.fn();

    store.subscribe("activity.finished", finished);
    store.subscribe("resync", resync);
    expect(source?.url).toBe("/api/events");
    expect(store.connectionState).toBe("connecting");

    source?.emitError();
    expect(store.connectionState).toBe("reconnecting");

    source?.emitOpen();
    expect(store.connectionState).toBe("open");
    expect(resync).toHaveBeenCalledTimes(1);

    source?.emit("activity.finished", { activity: { id: "activity-1" } });
    expect(finished).toHaveBeenCalledWith({ activity: { id: "activity-1" } });

    source?.emitError();
    source?.emitOpen();
    expect(store.connectionState).toBe("open");
    expect(resync).toHaveBeenCalledTimes(2);
  });
});
