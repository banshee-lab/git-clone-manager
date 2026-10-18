import { SvelteSet } from "svelte/reactivity";
import type { ActivityStatus } from "./api/types";
import type { EventSourceFactory } from "./events.svelte";

export type LogConnectionState =
  "connecting" | "open" | "reconnecting" | "closed";

interface StreamSource {
  onopen: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
  addEventListener(type: string, listener: EventListener): void;
  close(): void;
}

type LogEvent = { log: string; offset: number; status: ActivityStatus };
type ChunkEvent = { chunk: string; offset: number };
type EndEvent = {
  status: Extract<ActivityStatus, "success" | "failed" | "interrupted">;
};

function parse<T>(event: Event): T | undefined {
  try {
    return JSON.parse((event as MessageEvent<string>).data) as T;
  } catch {
    return undefined;
  }
}

export function createLogStream(
  activityId: string,
  factory?: EventSourceFactory,
) {
  let log = $state("");
  let offset = $state(0);
  let activityStatus = $state<ActivityStatus | undefined>(undefined);
  let connectionState = $state<LogConnectionState>("connecting");
  let source: StreamSource | undefined;
  const subscribers = new SvelteSet<() => void>();

  function notify(): void {
    subscribers.forEach((subscriber) => subscriber());
  }

  const makeSource =
    factory ??
    ((url: string) => new EventSource(url) as unknown as StreamSource);
  source = makeSource(
    `/api/activities/${encodeURIComponent(activityId)}/stream`,
  );

  source.onopen = () => {
    connectionState = "open";
    notify();
  };
  source.onerror = () => {
    connectionState = "reconnecting";
    notify();
  };
  source.addEventListener("snapshot", (event) => {
    const data = parse<LogEvent>(event);
    if (
      !data ||
      typeof data.log !== "string" ||
      typeof data.offset !== "number"
    )
      return;
    log = data.log;
    offset = data.offset;
    activityStatus = data.status;
    notify();
  });
  source.addEventListener("log", (event) => {
    const data = parse<ChunkEvent>(event);
    if (
      !data ||
      typeof data.chunk !== "string" ||
      typeof data.offset !== "number"
    )
      return;
    if (data.offset <= offset) return;
    log += data.chunk;
    offset = data.offset;
    notify();
  });
  source.addEventListener("end", (event) => {
    const data = parse<EndEvent>(event);
    if (!data) return;
    activityStatus = data.status;
    close();
    notify();
  });

  function close(): void {
    if (!source) return;
    source.close();
    source = undefined;
    connectionState = "closed";
  }

  function subscribe(handler: () => void): () => void {
    subscribers.add(handler);
    return () => subscribers.delete(handler);
  }

  return {
    get log(): string {
      return log;
    },
    get offset(): number {
      return offset;
    },
    get activityStatus(): ActivityStatus | undefined {
      return activityStatus;
    },
    get connectionState(): LogConnectionState {
      return connectionState;
    },
    subscribe,
    close,
  };
}
