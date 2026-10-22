# Security review

This checklist records the Phase 14 review against the implementation and its regression tests. The review found one input-boundary issue: repository URLs accepted embedded HTTP credentials even though the repository URL is returned by the authenticated API. Validation now rejects URL user-info so credentials must use the access-token field (`internal/api/api.go`; `TestValidateRepositoryInputRules`).

**Developer review:** complete. **Independent reviewer sign-off:** pending.

## Checklist

| Area | Result and evidence |
|---|---|
| Secrets in repository API responses | Access tokens are represented only by `has_access_token`; the canary serialization check is `TestRepositoryOutputOmitsAccessToken`. Repository webhook secrets are intentionally returned to authenticated admins because the product contract requires them to be copyable/editable; they are never returned from unauthenticated webhook responses. `TestRepositoryOutputIncludesWebhookSecret` protects that documented behavior. |
| Secrets in logs and notifications | Git output passes through a streaming redactor before writing to the log file, including Basic-auth forms and matches split across writes (`TestRedactorMasksAcrossWrites`, `TestRedactorMasksBasicCredential`, `TestSubmitPersistsRedactedOutputAndPublishesLifecycle`). Notification failure tests assert bot tokens and remote response bodies are absent (`TestTelegramRequestEscapingAndSecretSafeFailure`). Webhook logs contain only ID/source/outcome (`TestHandlerLogsOutcomeWithoutPayload`). |
| OIDC/session secrets in post commands | Commands receive an allowlisted environment, not the server environment. OIDC/server/webhook/notification namespaces are denied even if named in passthrough (`TestExecutorPostCommandEnvironmentAndFailure`). |
| Cookie and login protections | Session cookies are encrypted and signed, `HttpOnly`, `SameSite=Lax`, and `Secure` for an HTTPS base URL. Login uses state, nonce and PKCE; `next` is limited to a relative path (`TestService_LoginCallbackRoundTrip`, `TestCallbackRejectsInvalidStateAndTransientCookie`, `TestSanitizeNext`, `TestLoginTransientCookieSecurityDependsOnBaseURL`). |
| CSRF | `http.CrossOriginProtection` wraps the application mux, including unsafe `/api` operations. Only `/hooks/` is bypassed because each hook authenticates independently (`TestCrossOriginProtection`, `TestWebhookRouteNeedsNoSessionAndBypassesCSRF`). |
| Webhook authentication | GitHub uses HMAC-SHA256 and `hmac.Equal`; GitLab and generic tokens use constant-time comparison. Missing configuration fails closed, bodies are capped at 25 MiB, and request payloads are not logged (`TestHandlerAuthentication`, `TestHandlerAuthFailureAndHeaderPrecedence`, `TestHandlerBodyLimitBusyAndAcceptedResponse`, `TestHandlerLogsOutcomeWithoutPayload`). |
| Clone paths and deletion | Repository paths must be normalized, absolute, outside `data_dir`, non-overlapping, and free of symlink components. Delete-files verifies the target contains a Git directory and is outside `data_dir` before removal (`TestValidateRepositoryInputRules`, `TestValidateDeletePathSafety`). Names are not used as filesystem paths; repository and activity log path segments must be UUID-shaped (`TestValidationMissingRemovalAndRepo`, `TestValidateUUIDv4ID`). |
| Log files | Logstore creates repository directories with mode `0750` and files with mode `0640`, validates IDs before path construction, masks Git access tokens before writes, and sweeps only regular files at its direct repository/activity layout (`TestCreateAppendReadAndModes`, `TestSweep`, `TestSubmitPersistsRedactedOutputAndPublishesLifecycle`). |
| Command and argument handling | Git is invoked with argv, not a shell. Repository URLs must be HTTP(S) without user-info; branches are validated and leading `-` is rejected before being passed as an option. Only administrator-supplied post commands intentionally run through `sh -c` (`TestExecutorClonePullAndDefaultBranch`, `TestExecutorExplicitBranchClone`, `TestExecutorPostCommandEnvironmentAndFailure`, `TestRunDoesNotPutTokenInArgv`). |

## Deployment limitations and mitigations

- `/hooks/*` has no built-in rate limit. Apply request and connection limits at a reverse proxy.
- Post commands run with the container user's privileges and can access the clone and mounted volumes. Treat repository contents and configured commands as trusted code.
- Mounting `/var/run/docker.sock` grants root-equivalent control over the host. Do not mount it unless a trusted post command requires it.
- Activity logs are plain files and may contain sensitive command output unrelated to access tokens. Restrict volume access and backups.
- GitSync supports one process per bbolt data directory. Do not point multiple replicas at the same database.

## Scope note

Webhook secrets are visible in the authenticated repository API by explicit product requirement so administrators can configure their provider. Protect admin accounts and use TLS. OIDC client/session secrets, notification credentials, and Git access tokens are not included in API output. Local Compose credentials are development-only.
