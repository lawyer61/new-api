# Implementation Notes

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

## Other things that user need to note
- Local Go commands need `GOROOT` unset in this environment because the inherited value points to an older standard library.
- Remote GitHub Actions status is recorded after the merge commit is pushed.
