# Implementation Notes

## 2026-10-07 idle database mode

### Design decisions
- Added the opt-in startup environment switch `IDLE_DB_MODE`, default `false`. Read it after `InitResources()` so `.env` loading still works. Changing it requires a process restart; this is not a dashboard option.
- Kept the change in existing startup call sites instead of adding a scheduler abstraction or changing individual services. Unset/false retains the upstream startup behavior.
- Intended deployment: one master instance serving ordinary API relay, with management changes made through that instance. Do not use when subscription maintenance, Codex OAuth renewal, background system tasks, asynchronous task polling, or cross-node configuration/authorization propagation is required.
- Retained initial DB/migration, option, authorization, channel-cache and OAuth loading. In idle mode, load persisted task plugins once synchronously before serving requests, without their 30-second reload loop.
- Retained security cleanup and request-driven persistence. Batch billing and quota-dashboard flush loops return without SQL when their queues are empty; disabling those loops could discard pending accounting data. Performance-metric flushes also remain enabled.

### Modules
- `main.go`: guards channel/config/plugin/policy polling, automatic channel-balance updates, Codex refresh, subscription maintenance, instance reporting, scheduled-task registration and the system-task runner.
- `.env.example`: documents the switch, restart requirement, supported deployment and background features it pauses.
- No frontend, database schema/driver, billing calculation, plugin metadata or workflow changes.

### How to run
```bash
env -u GOROOT GOWORK=off go build -o /tmp/new-api-idle-20261007/new-api .
# Native deployment; for Docker/Compose add the same variables to the service environment.
IDLE_DB_MODE=true MEMORY_CACHE_ENABLED=true /tmp/new-api-idle-20261007/new-api
# Remove IDLE_DB_MODE or set it to false and restart to restore ordinary background work.

env -u GOROOT GOWORK=off go vet ./...
env -u GOROOT GOWORK=off make test
source /tmp/new-api-sync-20261007/env.sh
TEST_MYSQL_DSN="$MYSQL_MODEL_DSN" TEST_POSTGRES_DSN="$POSTGRES_MODEL_DSN" env -u GOROOT GOWORK=off go test -count=1 ./model
TEST_MYSQL_DSN="$MYSQL_CONTROLLER_DSN" TEST_POSTGRES_DSN="$POSTGRES_CONTROLLER_DSN" AUDIT_MYSQL_DSN="$MYSQL_CONTROLLER_DSN" AUDIT_POSTGRES_DSN="$POSTGRES_CONTROLLER_DSN" env -u GOROOT GOWORK=off go test -count=1 ./controller -run '(DatabaseMatrix|Migration)'
python3 /tmp/new-api-idle-20261007/verify_idle.py
```

### Implemented
- One reversible switch pauses the analyzed routine DB pollers while preserving startup state and the existing local management refresh paths.
- Kept task adaptor wiring, Redis subscriber, monitoring, authentication checks, audit/persistence, batch billing and quota-dashboard persistence unchanged.

### Not implemented / known limitations
- Not a zero-SQL or automatic sleep/wake mode. The current fork's master-only authentication cleanup performs SQL at startup and hourly. Requests, management/monitoring clients, queued data flushes, and performance-metric retention cleanup (`retention_days > 0`) can still access DB.
- External DB writes and changes from other nodes are not periodically propagated while idle mode is enabled; restart or use the existing explicit local reload path. Direct database edits are not the supported management path for this mode.
- System tasks remain durable but do not execute until ordinary mode is restored. This includes manual log cleanup jobs and asynchronous task completion/refund polling. No on-demand scheduler or alternate settlement path was added.
- No schema changes, so a new fresh/latest-release migration matrix is not required for this switch; real SQLite/MySQL/PostgreSQL runtime and existing database matrices are required and recorded below.

### Observed results
- Root vet/build, independent relaykit vet/build and `make test` passed. `make test` excludes the embedded-web main package; the real-binary runtime probe below covers the changed startup behavior instead of adding a layout/assertion-only test.
- Fresh, non-cached auth-policy, expired/revoked credential, auth-cleanup, local plugin refresh and external fallback regression tests passed: `env -u GOROOT GOWORK=off go test -count=1 ./service/authz ./service ./middleware ./controller -run 'Test(SetUserPermissions|.*Expired.*|.*Revok.*|.*AuthArtifacts.*|UploadTaskPluginRefreshesRuntimeSyncState|ActivateTaskPluginRefreshesRuntimeSyncState|SyncTaskPluginsPublishesOneGenerationForWholeBatch|RelayUsesFallbackModelSelectedByDistributor)'`.
- Real database versions: SQLite 3.41.2, MySQL 8.0.46-0ubuntu0.24.04.4, PostgreSQL 16.15-0ubuntu0.24.04.1. Complete model matrix passed (11.465s); controller database/migration matrix passed (6.387s), using the commands above.
- All nine real-process scenarios passed: each engine with true, false and unset. Every scenario loads a stored SystemName, a channel and a stored native task plugin; the plugin query returns persisted data, a chat request reaches a local HTTP upstream, and pending batch billing is flushed to the user row. Separate MySQL/PostgreSQL log databases are configured.
- The probe uses `MEMORY_CACHE_ENABLED=true`, `SYNC_FREQUENCY=1`, `BATCH_UPDATE_ENABLED=true`, empty queues after the request flush, and no HTTP requests during each 65-second observation. Counts are executed ORM SQL statements from DEBUG traces, not a claim about all deployments or indefinite idle operation:

  | Engine | `true` | `false` | Unset |
  | --- | ---: | ---: | ---: |
  | SQLite | 0 | 340 | 341 |
  | MySQL | 0 | 342 | 342 |
  | PostgreSQL | 0 | 340 | 342 |

- Ordinary modes show option/channel/policy/plugin/instance/system-task SQL; idle mode shows none in those windows. The hourly security cleanup and non-default performance retention remain documented exceptions. Results: `/tmp/new-api-idle-20261007/idle-results.json`; full private traces are in the same off-repo directory.

### Other things that user need to note
- Security references: [OWASP ASVS 5.0.0](https://owasp.org/www-project-application-security-verification-standard/), [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html), [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html). The change retains server-side initialization, authorization, expiry/revocation checks and credential/session cleanup. Local permission management commits then reloads policy; multi-node/out-of-band revocation is explicitly outside this mode. This is not a full-project ASVS audit.
- The idle-runtime probe and its temporary databases/logs are outside the repository. Local ctx search remains unavailable; current code and existing records were used instead.
- `.github.env` and the user-provided `task_mitigate_db_access.md` must remain untracked and absent from `.gitignore`; neither is part of this commit or the remote Docker context.

## 2026-10-07 upstream sync

### Design decisions
- Fast-forwarded to the existing fork `origin/main` at `c64642f10`, then merged canonical upstream `78bd5b1cbf7bffc462a515a6c6e27567ace7a4d8` without rewriting history.
- Kept upstream's permission-route registration and reattached the fallback route with one registration call. Preserved the distributor-to-relay fallback handoff, public/attempt model separation, model mapping, retries, and audit metadata.
- Used upstream locale files as the base, restored exactly the 22 existing fallback keys through the project translation script, and ran i18n sync. No upstream translation value was overridden.
- Reused the existing system-settings token scopes for fallback administration: `option:read` for GET, `option:write` for PUT and POST test. The existing RootAuth requirement remains unchanged; no new scope, authentication mechanism, or generic extension framework was introduced.

### Modules
- `router/api-router.go`: retains the fork route alongside upstream's scoped permission registration.
- `middleware/access_token_routes.go`: declares the three fallback administration routes in upstream's new access-token rule table.
- `middleware/auth_test.go`: verifies successful scoped Root requests and denial for read-only writes, unrelated scopes, non-Root roles, expired tokens, and revoked tokens.
- `web/src/i18n/locales/*.json`: preserves all seven languages and their existing fallback translations.

### How to run
```bash
env -u GOROOT GOWORK=off go vet ./...
env -u GOROOT GOWORK=off go build ./...
(cd relaykit && env -u GOROOT GOWORK=off go vet ./... && env -u GOROOT GOWORK=off go build ./...)
env -u GOROOT GOWORK=off make test
TEST_MYSQL_DSN="$MYSQL_MODEL_DSN" TEST_POSTGRES_DSN="$POSTGRES_MODEL_DSN" env -u GOROOT GOWORK=off go test -count=1 ./model
TEST_MYSQL_DSN="$MYSQL_CONTROLLER_DSN" TEST_POSTGRES_DSN="$POSTGRES_CONTROLLER_DSN" AUDIT_MYSQL_DSN="$MYSQL_CONTROLLER_DSN" AUDIT_POSTGRES_DSN="$POSTGRES_CONTROLLER_DSN" env -u GOROOT GOWORK=off go test -count=1 ./controller -run '(DatabaseMatrix|Migration)'
(cd web && bun install --frozen-lockfile && bun run i18n:sync && bun run typecheck && bun run test && bun run build)
```

### Implemented
- Resolved the router and seven locale conflicts while preserving fork functionality at narrow seams.
- Fixed the semantic integration gap detected by upstream's route-coverage test: fallback routes otherwise had no access-token declaration and would reject valid scoped requests.
- Kept the existing GitHub upstream-sync/GHCR workflow and its push-triggered image build.

### Not implemented / known limitations
- Repository-wide frontend lint and format checks still report upstream-only files; all reported paths were compared with `upstream/main` and none contain fork differences. No unrelated mass cleanup was added. The four fork-owned settings TypeScript files pass their targeted checks.
- Local ctx history search is unavailable because no verified index/importable source is configured; integration used the supplied conversation context, Git history, and existing implementation notes.

### Observed results
- The external fallback relay/model-mapping regressions passed, as did complete middleware/router tests and the new scope/role/expiry/revocation test.
- Root and independent relaykit vet/build checks and the final `env -u GOROOT GOWORK=off make test` passed. A first controller suite exceeded its default ten-minute timeout during local SQLite filesystem sync under concurrent load; the active test passed alone in 1.902s, and the complete controller suite passed on rerun in 37.150s. No permanent timeout or production behavior workaround was added.
- Frontend typecheck/build passed; all 173 test files and 2,161 tests passed. i18n has zero missing/extra keys; remaining untranslated entries are upstream provider/product names and `Responses WebSocket`.
- Real database versions: SQLite 3.41.2, MySQL 8.0.46-0ubuntu0.24.04.4, PostgreSQL 16.15-0ubuntu0.24.04.1. Model and controller three-database matrices passed; task-plugin/settlement paths also passed with `TEST_TASK_DB_DIALECT=mysql` and `postgres`.
- All six fresh/latest-release (`v1.0.0-rc.41`) upgrade startup scenarios passed. Each version started at least twice; main/log markers, indexes/constraints and a 70,004-byte plugin source survived. Separate MySQL/PostgreSQL log databases were included. Schema snapshots were stable; MySQL's mutable next-ID counters were excluded after confirming that only conflict-safe role seeding changed them. Fresh MySQL/PostgreSQL had 37 main tables and 2 log tables; SQLite had 38 combined tables.
- Startup matrix command: `source /tmp/new-api-sync-20261007/env.sh && SYNC_DB_PREFIX="${SYNC_DB_USER}_v2" python3 /tmp/new-api-sync-20261007/verify_startup.py`; result log: `/tmp/new-api-sync-20261007/startup-matrix-final.log`. All validation scripts and credentials stay outside the repository.

### Other things that user need to note
- Security references: [OWASP ASVS 5.0.0](https://owasp.org/www-project-application-security-verification-standard/), [Authentication Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html), and [Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html). This scoped integration preserves server-side role/scope enforcement, fail-closed rules, upstream credential hashing/audit behavior, and existing session handling; it is not a claim of a full-project ASVS audit.
- `.github.env` remains untracked, absent from `.gitignore`, and excluded from the Docker build context by existing env-file patterns.

## 2026-09-18 fallback relay regression fix

### Design decisions
- Restored the fallback handoff only in `controller.Relay`, immediately after shared request billing and before the ordinary random-channel retry loop.
- Reused the existing `runFallbackRelay` path instead of duplicating channel selection or changing upstream's shared retry selector. This keeps the fork-specific seam small for future upstream merges.
- Kept upstream's centralized billing refund and performance-result defers; the obsolete pre-merge fallback metrics special case was not restored.

### Modules
- `controller/relay.go`: consumes the fallback model selected by `middleware.Distribute` and runs its configured attempts.
- `controller/fallback_relay_test.go`: covers the external chat-completions path from distributor selection through a real fallback upstream request.

### How to run
```bash
env -u GOROOT GOWORK=off go test ./controller -run '^TestRelayUsesFallbackModelSelectedByDistributor$' -count=2
env -u GOROOT GOWORK=off go vet ./...
env -u GOROOT GOWORK=off go build ./...
env -u GOROOT GOWORK=off make test
```

### Implemented
- Fixed the post-merge regression where the distributor recognized a fallback model, but `controller.Relay` ignored that context and entered ordinary channel selection for the public fallback name.
- Added a regression test that previously reproduced `分组 default 下模型 auto 的可用渠道不存在（retry）` and now verifies the configured fallback channel receives the mapped upstream model.

### Not implemented / known limitations
- No database, frontend, fallback configuration format, or provider-specific behavior changed.

### Observed results
- The regression test failed deterministically twice before the fix with the reported no-channel retry error, then passed twice after the fix.
- Controller and middleware package tests passed.
- Root `go vet`, `go build ./...`, and `make test` passed; `make test` also passed the independent `relaykit` test suite.

### Other things that user need to note
- `.github.env` remains untracked and is not ignored.

## 2026-09-18 upstream sync

### Integration decisions
- Fast-forwarded local `main` to the newer `origin/main`, then merged `upstream/main` at `3524fe0b15794d8d19378827d36a7edc0b0e91ea`.
- Resolved conflicts by taking the upstream routing, audit, relay-error, and locale structures, then restoring fallback-model behavior only at narrow seams.
- Kept fallback selection as a small pre-selection branch in `middleware/distributor.go`; normal and pinned requests continue through upstream's shared `SelectChannelForRequest` path.
- Switched fallback attempts to upstream's shared `AppendUsedChannel`, retry-policy, failure-audit, and channel-error helpers instead of retaining copied controller logic.
- Preserved exactly the 22 fallback-model UI translation keys. Two stale, unused fork-only locale keys remain dropped.

### Verification
- Backend CI-equivalent checks passed:
  ```bash
  env -u GOROOT GOWORK=off go vet ./...
  (cd relaykit && env -u GOROOT GOWORK=off go vet ./...)
  env -u GOROOT GOWORK=off go build ./...
  (cd relaykit && env -u GOROOT GOWORK=off go build ./...)
  env -u GOROOT GOWORK=off make test
  ```
- Frontend dependency install, typecheck, production build, and lint/format checks for the four fork-owned TypeScript files passed.
- Full frontend test result: 149 files and 1,907 tests passed; one upstream test failed: `src/features/usage-logs/components/__tests__/group-filter.test.tsx` expects a sensitive dropdown to remain inside its masked field, while upstream commit `0cde9d94f6` now portals that dropdown to `document.body`. The production and test files match `upstream/main`; no unrelated fork patch was added.
- Repository-wide frontend lint and format checks still fail on upstream files. The failures are outside the fallback-model integration, so they were not mass-fixed.
- `bun run i18n:sync` reported zero missing and extra keys. Its remaining untranslated entries are upstream product names/terms (`SGLang`, `Zhipu GLM`, and `Responses WebSocket`).

### Database verification
- Engines: SQLite `3.41.2`, MySQL `8.0.46-0ubuntu0.24.04.4`, PostgreSQL `16.15-0ubuntu0.24.04.1`.
- Real-database model and controller matrix tests passed with `TEST_MYSQL_DSN` and `TEST_POSTGRES_DSN`, including the controller's isolated-database matrix.
- Fresh startup/migration was repeated until schema snapshots were stable on all three engines. Results: SQLite 37 combined tables; MySQL and PostgreSQL 36 main tables plus 2 tables in their separately configured log databases.
- Upgrade verification used release tag `v1.0.0-rc.37` (`385d2dfd10d821b25c8a6766bd16eea248cb1652`). The release binary was started twice, marker rows were inserted into `options` and the separate `logs` database, then the merged binary was started twice. All markers, indexes, constraints, and schema snapshots were preserved and stable on SQLite, MySQL, and PostgreSQL.

### Remote status
- Merge commit `e4591ecaaeab7317d991cf2520659e58cf976bad` was pushed to `origin/main`.
- GitHub Actions run `35343155980` completed successfully. Its `Build Docker image` job built and pushed the `main`, commit-SHA, and `latest` GHCR tags: <https://github.com/lawyer61/new-api/actions/runs/35343155980>.

## Design decisions
- Merge `upstream/main` instead of rewriting fork history.
- Prefer upstream implementations in conflicts, then reattach fallback-model behavior at narrow extension points.
- Keep fallback route registration and relay helper methods in dedicated files to reduce future overlap with upstream edits.

## Modules
- `controller/fallback_relay.go`: runs configured fallback attempts against current relay handlers.
- `relay/common/fallback_routing.go`: exposes fallback public and per-attempt model identities.
- `router/fallback-model-router.go`: registers fallback-model administration routes outside the upstream API router body.
- `service/log_info_generate.go`: records fallback attempt metadata through upstream's scoped log metadata API.
- `web/src/i18n/locales/*.json`: retains the 22 fallback-model UI keys across all supported locales.

## How to run
```bash
env -u GOROOT GOWORK=off go vet ./...
(cd relaykit && env -u GOROOT GOWORK=off go vet ./...)
env -u GOROOT GOWORK=off go build ./...
(cd relaykit && env -u GOROOT GOWORK=off go build ./...)
env -u GOROOT GOWORK=off make test
cd web
bun install --frozen-lockfile
bun run i18n:sync
bun run typecheck
bun run test
bun run build
```

## Implemented
- Added and fetched the canonical `upstream` remote.
- Merged the latest upstream `main` and resolved all conflicts.
- Preserved fallback-model routing, billing identity, error logging, API routes, and translations.
- Kept `.github.env` untracked and intentionally absent from `.gitignore`.

## Not implemented / known limitations
- The repository-wide frontend lint command currently reports pre-existing upstream lint errors outside the fallback-model changes; they were not mass-fixed to avoid unrelated fork drift.

## Observed results
- Root and independent `relaykit` vet/build checks passed.
- Root and `relaykit` test suites passed.
- Frontend typecheck passed; 60 test files and 408 tests passed.
- Frontend production build passed.
- Frontend i18n report shows zero missing, extra, or untranslated keys in all seven locales.

## Database verification
- Engines: SQLite `3.41.2`, MySQL `8.0.46-0ubuntu0.24.04.4`, PostgreSQL `16.15-0ubuntu0.24.04.1`.
- Real-database migration tests passed:
  ```bash
  TEST_MYSQL_DSN="$MYSQL_MODEL_DSN" TEST_POSTGRES_DSN="$POSTGRES_MODEL_DSN" env -u GOROOT GOWORK=off go test -count=1 ./model
  TEST_MYSQL_DSN="$MYSQL_CONTROLLER_DSN" TEST_POSTGRES_DSN="$POSTGRES_CONTROLLER_DSN" env -u GOROOT GOWORK=off go test -count=1 ./controller
  ```
- Fresh databases: the current binary completed startup, migration, `/api/status`, and graceful shutdown repeatedly on all three engines. MySQL and PostgreSQL used separate main and log databases. Their schema snapshots were unchanged on the second current-version run. SQLite needed a third run because the second run canonicalized quoting in several `CREATE TABLE` definitions; the second and third schema snapshots were identical.
- Upgrade databases: built tag `v1.0.0-rc.31`, started it twice, inserted marker rows in `options` and `logs`, then started the merged binary twice. All three engines preserved both markers, and schema snapshots were unchanged between the two merged-version runs. MySQL/PostgreSQL separate log databases were included.
- Fresh and upgraded databases contained 36 main tables; separately configured MySQL/PostgreSQL log databases contained one log table.

## Remote verification
- Merge commit `f7f394c8833b353de83c84395baac548acfbc1a5` was pushed to `origin/main`.
- GitHub Actions run `33851312554` (`Sync upstream and build Docker image`) completed successfully: <https://github.com/lawyer61/new-api/actions/runs/33851312554>.
- A manual upstream-sync verification also passed: run `33855656442` completed its `Merge upstream` step successfully and correctly skipped image builds because upstream had no newer commit: <https://github.com/lawyer61/new-api/actions/runs/33855656442>.
- `.github/workflows/ci.yml` only runs for pull requests, so the direct push did not create a separate CI run; its vet/build/test commands were run locally and passed.

## Other things that user need to note
- Local Go commands need `GOROOT` unset in this environment because the inherited value points to an older standard library.
