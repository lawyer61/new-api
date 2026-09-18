# Implementation Notes

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
- Push and image-build workflow results are recorded after the merge commit is published.

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
