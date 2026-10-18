import "@testing-library/jest-dom/vitest";
import { afterEach } from "vitest";
import { FakeEventSource } from "./FakeEventSource";

Object.defineProperty(globalThis, "matchMedia", {
  configurable: true,
  value: (media: string) =>
    ({
      matches: false,
      media,
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    }) as MediaQueryList,
});

Object.defineProperty(globalThis, "EventSource", {
  configurable: true,
  value: FakeEventSource as unknown as typeof EventSource,
});

afterEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove("dark");
  FakeEventSource.reset();
});
