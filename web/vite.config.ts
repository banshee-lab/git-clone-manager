import { fileURLToPath } from "node:url";
import adapter from "@sveltejs/adapter-static";
import { sveltekit } from "@sveltejs/kit/vite";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    tailwindcss(),
    sveltekit({
      adapter: adapter({
        fallback: "index.html",
        strict: false,
      }),
      preprocess: vitePreprocess(),
    }),
  ],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/auth": "http://localhost:8080",
      "/hooks": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.{test,spec}.{js,ts}"],
    clearMocks: true,
    alias: [
      {
        find: /^svelte$/,
        replacement: fileURLToPath(
          new URL("./node_modules/svelte/src/index-client.js", import.meta.url),
        ),
      },
    ],
    server: {
      deps: {
        inline: [
          "@runwisp/ui",
          "@runwisp/common",
          "@testing-library/svelte",
          "@testing-library/svelte-core",
          "svelte",
        ],
      },
    },
  },
});
