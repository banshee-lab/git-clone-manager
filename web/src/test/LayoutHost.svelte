<script lang="ts">
  import type { ApiMeta, SessionUser } from "#lib/api/types";
  import Layout from "../routes/+layout.svelte";

  const data = {
    user: {
      sub: "test-admin",
      email: "admin@example.test",
      name: "Test admin",
      role: "admin",
    } satisfies SessionUser,
    meta: {
      version: "test",
      base_url: "http://localhost:8080",
      history_max_items: 50,
      repos_root: "/repos",
      global_webhook_secret_configured: false,
    } satisfies ApiMeta,
    bootstrapError: null,
  };
</script>

<Layout {data} params={{}}>
  <p>Test route outlet</p>
</Layout>
