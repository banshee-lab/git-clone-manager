import { SvelteMap, SvelteSet } from "svelte/reactivity";

export type EventConnectionState = "connecting" | "open" | "reconnecting";
export type EventHandler = (data: unknown) => void;

interface EventSourceLike {
  onopen: ((event: Event) => void) | null;
  onerror: ((event: Event) => void) | null;
  addEventListener(type: string, listener: EventListener): void;
  removeEventListener(type: string, listener: EventListener): void;
  close(): void;
}

export type EventSourceFactory = (url: string) => EventSourceLike;

function browserEventSource(url: string): EventSourceLike {
  return new EventSource(url);
}

function parseEventData(data: string): unknown {
  try {
    return JSON.parse(data) as unknown;
  } catch {
    return data;
  }
}

export function createEventsStore(
  factory: EventSourceFactory = browserEventSource,
) {
  let connectionState = $state<EventConnectionState>("connecting");
  let source: EventSourceLike | undefined;
  const subscriptions = new SvelteMap<string, SvelteSet<EventHandler>>();
  const eventListeners = new SvelteMap<string, EventListener>();

  function dispatch(type: string, data: unknown): void {
    subscriptions.get(type)?.forEach((handler) => handler(data));
  }

  function attach(type: string): void {
    if (!source || eventListeners.has(type)) return;
    const listener: EventListener = (event) => {
      dispatch(type, parseEventData((event as MessageEvent<string>).data));
    };
    eventListeners.set(type, listener);
    source.addEventListener(type, listener);
  }

  function connect(): void {
    if (source) return;
    connectionState = "connecting";
    const nextSource = factory("/api/events");
    source = nextSource;
    nextSource.onopen = () => {
      if (source !== nextSource) return;
      connectionState = "open";
      dispatch("resync", { type: "resync" });
    };
    nextSource.onerror = () => {
      if (source === nextSource) connectionState = "reconnecting";
    };
    subscriptions.forEach((_handlers, type) => attach(type));
  }

  function close(): void {
    if (!source) return;
    eventListeners.forEach((listener, type) =>
      source?.removeEventListener(type, listener),
    );
    eventListeners.clear();
    source.close();
    source = undefined;
    connectionState = "connecting";
  }

  function subscribe(type: string, handler: EventHandler): () => void {
    const handlers = subscriptions.get(type) ?? new SvelteSet<EventHandler>();
    handlers.add(handler);
    subscriptions.set(type, handlers);
    connect();
    attach(type);

    return () => {
      handlers.delete(handler);
      if (handlers.size === 0) {
        subscriptions.delete(type);
        const listener = eventListeners.get(type);
        if (listener) source?.removeEventListener(type, listener);
        eventListeners.delete(type);
      }
    };
  }

  return {
    get connectionState(): EventConnectionState {
      return connectionState;
    },
    connect,
    close,
    subscribe,
  };
}

export const events = createEventsStore();
