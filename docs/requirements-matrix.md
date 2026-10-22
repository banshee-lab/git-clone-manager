# Requirements traceability matrix

This matrix maps the original brief and plan to implementation and regression coverage. Docker targets provide the authoritative full backend and frontend suites (`go-test`, `web-test`); focused test names are listed where they exercise the requirement directly. Browser and fresh-clone deployment checks are manual and are tracked in [acceptance](acceptance.md).

| Requirement | Implementation | Verification |
|---|---|---|
| Create and edit repositories with URL, branch, path, token, post command | Phases 7, 11: `internal/api`, repository forms | `TestValidateRepositoryInputRules`; `TestApplyRepositoryUpdatePreservesOmittedValues`; `keeps, replaces or clears access and webhook secrets correctly`; browser cases `ACC-02`, `ACC-03` |
| Remove repository; optionally remove clone files safely | Phases 7, 3a | `TestValidateDeletePathSafety`; `TestDeleteRepositoryAndStartupSweep`; browser case `ACC-08` |
| Manual sync for one repository and all repositories | Phases 5, 7, 11 | `TestSubmitAllSkipsBusyRepository`; `TestConcurrencyLimitQueuesWorkAcrossRepositories`; dashboard test `calls syncRepository with the repository id`; browser case `ACC-04` |
| Git clone, fetch, fast-forward merge, optional hard reset | Phase 4 | `TestExecutorClonePullAndDefaultBranch`; `TestExecutorExplicitBranchClone`; `TestExecutorFastForwardFailureAndHardReset`; `TestExecutorDirtyBranchSwitchRequiresHardReset` |
| Post-sync command, changed flag, timeout, sanitized environment | Phase 4 | `TestExecutorPostCommandEnvironmentAndFailure`; `TestExecutorPostCommandTimeout` |
| Webhook sources: GitHub, GitLab, generic | Phase 8 | `TestHandlerAuthentication`; `TestHandlerProviderSamplePayloads`; `TestHandlerEventFiltering` |
| Webhook branch/tag filtering, bounded body, fail-closed auth | Phase 8 | `TestHandlerEventFiltering`; `TestHandlerBodyLimitBusyAndAcceptedResponse`; `TestHandlerAuthFailureAndHeaderPrecedence` |
| Activity records trigger/source/status/actor/time/summary/commits | Phases 3, 5, 7 | `TestActivityEnumsAndMetadataOnly`; `TestSubmitPersistsRedactedOutputAndPublishesLifecycle`; API activity tests |
| Console output stored in files under configurable `storage.logs_dir` | Phases 3a, 7 | `TestCreateAppendReadAndModes`; `TestLogsDirectoryOverrideAndEnvParseError`; `TestFinishedActivityStreamSkipsSnapshotOnResume` |
| Keep `history.max_items` finished activities per repository; delete old records and files | Phases 3a, 5 | `TestPruneRemovesOldestFinishedAndLogsOnly`; `TestPruneFailureKeepsRecordAndRetries`; `TestCleanupPrunesOldestRecordsAndLogsPerRepository` |
| Startup recovers interrupted activity and removes orphan logs | Phases 3a, 5 | `TestDeleteRepositoryAndStartupSweep`; `TestRecoverMarksStaleActivitiesAndCreatesLog` |
| Dashboard repository count and recent activity | Phase 12 | dashboard route test `renders live, attention, recent panels and retries a failed repository`; store test `starts empty and updates from refresh` |
| Needs-attention panel includes failures and interruptions | Phase 12 | dashboard store tests `adds a failed activity to attention`, `adds an interrupted activity to attention`, `clears attention when a later sync succeeds` |
| Running-now panel, live console, reconnect and completion states | Phases 5, 12 | `TestLiveLogReadsFileAndClosesSubscriber`; `TestLiveLogSnapshotAndBoundedOutputMemory`; route test `streams snapshot, chunks and end, keeps the completed console open, and restores focus` |
| Left repository panel; detail activity output; edit/delete/Sync now | Phase 11 | `TestGetRepositoryIncludesLatestAndRunningActivity`; browser cases `ACC-02` through `ACC-05` and `ACC-08` |
| UI updates from SSE | Phases 5, 7, 12 | `TestSlowSubscriberIsDroppedWithoutBlocking`; `TestSubscribeClosesOnCancellation`; frontend `dispatches named events and emits resync each time the connection opens`; browser cases `ACC-04`, `ACC-05`, `ACC-06` |
| OIDC only; all authenticated users are admin | Phase 6 | `TestService_LoginCallbackRoundTrip`; `TestRequireSession`; `TestSessionTamperedAndExpired`; browser cases `ACC-01`, `ACC-11` |
| OIDC state, nonce, PKCE, secure cookies and safe `next` | Phase 6 | `TestCallbackRejectsInvalidStateAndTransientCookie`; `TestCallbackRejectsInvalidIDTokenClaims`; `TestLoginTransientCookieSecurityDependsOnBaseURL`; `TestSanitizeNext` |
| YAML and environment config with env override and validation | Phase 2 | `TestLoadDefaultsAndPrecedence`; `TestLoadExpansionAndScopes`; `TestLoadExpansionErrorsAndUnknownKeys`; `TestValidationCollectsErrorsAndNamesKeys` |
| Secrets masked from repository API, logs, notifications and errors | Phases 2, 4, 7, 9, 14 | `TestRepositoryOutputOmitsAccessToken`; `TestRedactorMasksAcrossWrites`; `TestSubmitPersistsRedactedOutputAndPublishesLifecycle`; `TestTelegramRequestEscapingAndSecretSafeFailure`; `TestRedactedAndExample` |
| bbolt persistence and consistent indexes | Phase 3 | `TestBoltPersistence`; `TestBoltRandomizedIndexConsistency`; shared store contract suite |
| Go 1.27 module and race-tested backend | Phases 1–14 | Docker target `go-test` runs `go vet ./...` and `go test ./... -race` |
| SvelteKit, Svelte 5, `@runwisp/ui`, light/dark/auto, frontend tests | Phases 10–12 | Docker target `web-test` runs Svelte check, lint/format, and Vitest; layout test `renders the workspace shell and persists theme toggle choices` |
| Docker-only deployment; frontend/backend built in image; backend serves SPA and API | Phase 13 | Dockerfile `web`, `build`, and `final` stages; `TestHandlerServesEmbeddedFile`; `TestHandlerFallsBackToIndex`; Docker build target |
| Build base, runtime packages, non-root UID, persistent volumes | Phase 13 | Dockerfile inspection and Phase 13 image/Compose smoke checks recorded in `PROGRESS.md` |
| Compose includes local OIDC provider | Phase 13 | `docker compose config --quiet`; Phase 13 Dex discovery and OAuth smoke checks recorded in `PROGRESS.md` |
| Global Telegram and signed webhook notifications | Phase 9 | `TestTelegramRequestEscapingAndSecretSafeFailure`; `TestWebhookPayloadHeadersAndSignature`; `TestRetryPolicy`; `TestInterruptedNotificationUsesFortyLineLogTailAndDoesNotBlock` |
| Redacted startup configuration, `--version`, and structured log context | Phase 14 | `TestRedactedAndExample`; `go run ./cmd/gitsync --version`; log fields `component`, `repo_id`, and `activity_id` |
| GPL-3.0-or-later license | Phase 1 | `LICENSE` |

Manual acceptance across browsers, themes, and viewport sizes is not represented by unit tests. Run the checklist in [acceptance.md](acceptance.md) and record the operator, date, and results there before release.
