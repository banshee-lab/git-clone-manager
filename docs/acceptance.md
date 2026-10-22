# Release acceptance procedure

Run this procedure from a fresh clone on a Docker host. It checks the deployed stack and the complete operator-facing flow, including browsers that unit tests cannot cover. Use test repositories and credentials; do not use production secrets during acceptance.

## Build and start

1. Clone the repository into a clean working directory.
2. Confirm Docker Engine and the Compose plugin are available.
3. Ensure `dex` resolves to `127.0.0.1` on the host so the browser can reach the local issuer. On openSUSE 16, also read [deployment notes](deployment.md) for SELinux and volume ownership.
4. Run:

   ```sh
   docker compose up --build -d
   docker compose ps
   curl --fail http://localhost:8080/healthz
   ```

5. Run automated acceptance targets:

   ```sh
   docker build --target go-test .
   docker build --target web-test .
   docker compose config --quiet
   docker compose run --rm --no-deps gitsync --version
   ```

## Browser pass

Use current Chrome and Firefox. For each browser, complete every row below in desktop width and a narrow viewport (approximately 375 CSS px). Repeat the display checks with light, dark, and auto theme settings.

| Check | Result |
|---|---|
| `ACC-01` Sign in with the local Dex development user; confirm dashboard and repository navigation load. | ☐ |
| `ACC-02` Create a test repository; verify its initial clone activity, webhook URL/secret, and repository detail. | ☐ |
| `ACC-03` Edit branch/settings; verify blank token keeps the existing token and explicit clear removes it. | ☐ |
| `ACC-04` Trigger Sync now and Sync all; verify activity state and completion update without a page reload. | ☐ |
| `ACC-05` Open activity output; verify live console scroll, completion state, and historical log display. | ☐ |
| `ACC-06` Cause a safe test failure; verify Needs attention shows it, then clear attention with a successful sync. | ☐ |
| `ACC-07` Send a correctly signed generic webhook; confirm it queues an activity and invalid credentials are rejected. | ☐ |
| `ACC-08` Delete one repository while retaining its clone; delete another with the explicit delete-files option and verify both outcomes. | ☐ |
| `ACC-09` Verify responsive navigation, repository list/detail, forms, tables, and live panels remain usable at narrow width. | ☐ |
| `ACC-10` Verify light, dark, and system/auto themes render legibly and the selected manual theme persists after reload. | ☐ |
| `ACC-11` Log out; verify protected API/dashboard access requires login again. | ☐ |

## Webhook and restart pass

1. Configure a temporary provider or send a correctly authenticated generic `X-Token` request; confirm `202` and a queued activity.
2. Send a wrong token and an oversized request; confirm `401` and `413` respectively, with no payload in server logs.
3. Restart the Compose stack and confirm repository data, activity history, and log files persist.
4. Remove test repositories and test volumes after saving the acceptance result.

## Record results

Record the tested commit, date, operator, browser versions, Docker/Compose versions, and any failures below. A release acceptance is complete only when all browser rows pass in both browsers across the stated viewports and themes.

| Field | Value |
|---|---|
| Commit | _pending_ |
| Date / operator | _pending_ |
| Chrome version | _pending_ |
| Firefox version | _pending_ |
| Docker / Compose versions | _pending_ |
| Outcome / follow-up issues | _pending_ |
