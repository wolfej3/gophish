# Verification log

These checks originally couldn't run in the sandboxed dev environment (the
Docker control socket and local `listen`/`bind` were denied). They were run on
a Docker-capable machine on **2026-10-30** and all passed. Details below.

## Tenants feature (phase 4 #8, commit `a34de19`) — VERIFIED 2026-10-30

- `gofmt -l .` clean, `go build ./...`, `go vet ./...`, full **SQLite** suite: pass.
- **MySQL suite** — `mysql:8.4.6` on `:3307`, `sql_mode` cleared, then
  `GOPHISH_TEST_MYSQL_DSN='root:…@(127.0.0.1:3307)/gophish_test?charset=utf8&parseTime=True&loc=UTC' go test -p 1 ./...`:
  **all packages pass, 0 failures**, including the port-binding `controllers`,
  `controllers/api`, and `models` packages. The hand-written MySQL tenants
  migration runs during setup and `models/tenant_test.go` passes.
- **Migration down→up** against MySQL (goose v3.29.0, `db/db_mysql/migrations`):
  `up-to 20261105000000` created the `tenants` table + `tenant_id` on
  `teams`/`users`; `down` dropped them cleanly; `up` re-applied. Rollback SQL is
  correct.
- **Fresh-DB seeding** (checked directly in MySQL): first admin is
  `provider_admin` (role id 3), a Default tenant (id 1) and Default team (id 1)
  exist, and the admin's auto-created **Personal #1** team is present
  (`owner_user_id = 1`).

Remaining (UI-only, low risk): the hands-on multi-tenant *isolation* walkthrough
(create two tenants each with a tenant admin; confirm each tenant admin sees only
their own tenant's teams/users; a peer can't see another's personal team; the
default-team selector saves). The provider-admin view, tenant switcher, and
seeding were spot-checked in the browser; the underlying authorization logic is
covered by passing tests (`tenant_test.go`, the api-package
`TestTenantAdminCannotTouchProviderAdmin` / impersonation tests, and the scope
tests), all green on MySQL.

## Docker Compose stack (phase 4 #12) — VERIFIED 2026-10-30

- `docker compose config` parses the base file and the base+proxy overlay.
- `docker compose up -d --build`: image builds, MySQL comes up healthy, gophish
  migrates **to the latest version (`20261106000000`)** into MySQL — the
  `gophish` database holds 30 tables (`tenants`, `teams`, `sessions`,
  `audit_logs`, `goose_db_version`, …).
- **Login**: logged in as `admin` over the self-signed HTTPS admin server; the
  first-login forced password reset works.
- **MySQL persistence**: after `docker compose restart`, all tables, the admin
  user, and the Default tenant/teams survive (data lives in the `db_data`
  volume); no duplicate re-seed. The browser password reset also persisted.
- **Reverse-proxy overlay**
  (`docker compose -f docker-compose.yml -f docker-compose.proxy.yml up -d`):
  gophish drops to `http://:3333` (`ADMIN_USE_TLS=false`), Caddy terminates TLS
  on `:443` with its local CA (`CN=Caddy Local Authority`), and the admin login
  page is reachable through Caddy over HTTPS (200). CSRF accepts a proxied login
  via `ADMIN_TRUSTED_ORIGINS` (the POST reached the auth check rather than being
  rejected as a cross-origin request).

## Note observed during verification (not a blocker)

On the admin login **error paths** (CSRF reject, bad password) over the direct
self-signed HTTPS server, the gorilla access logger reports a `superfluous
response.WriteHeader call`, and a raw curl client can receive a `500` while the
access log records the correct status (403/401). A real browser logs in fine
(confirmed), and the proxied path doesn't exhibit it. Worth a small follow-up to
find the double `WriteHeader` on the login/CSRF error path, but it does not
affect normal use.
