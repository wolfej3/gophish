# Verification pending

Checks that still need to be run on a machine with Docker and local port
binding (they couldn't run in the sandboxed dev environment where this was
built — the Docker control socket and local `listen`/`bind` are both denied).

## Tenants feature (phase 4 #8, commit `a34de19`)

Already verified: `gofmt -l .` clean, `go build ./...`, `go vet ./...`, and the
full **SQLite** test suite (`go test ./...`) — the new tenants migration runs
during test setup, and `models/tenant_test.go` plus the updated team/user/
person/scope tests pass.

Still to run:

1. **MySQL tests** — exercises the hand-written MySQL migration
   (`db/db_mysql/migrations/20261105000000_0.15.0_tenants.sql`), which is the
   main untested piece.

   ```bash
   docker run --rm -e MYSQL_ROOT_PASSWORD=gophish-test -e MYSQL_DATABASE=gophish_test -p 3307:3306 mysql:8.4.6
   ```

   Then (after setting `sql_mode` to drop `NO_ZERO_DATE`):

   ```bash
   GOPHISH_TEST_MYSQL_DSN='root:gophish-test@(127.0.0.1:3307)/gophish_test?charset=utf8&parseTime=True&loc=UTC' go test -p 1 ./...
   ```

   Also worth a manual check: run the migration **down** then **up** against
   MySQL to confirm the rollback SQL is correct (tests only exercise `up`).

2. **Browser check** — via the local HTTP→HTTPS proxy (admin needs TLS). Verify:
   - Provider admin sees the **Tenants** page, the tenant switcher, and tenant
     columns on Teams/Users; can create/rename/delete a tenant.
   - Create two tenants with a tenant admin each; confirm each tenant admin sees
     only their own tenant's teams/users and the team switcher lists only
     in-tenant teams.
   - A regular user has a **Personal** team in the switcher and can't see a
     peer's personal team; a tenant admin can.
   - Default-team selector in Account Settings saves and takes effect on sign-in.
   - Fresh DB: first admin is `provider_admin`; a Default tenant/team exist.

Note: 5 pre-existing tests (`sender`, `webhook`, `directory`, and two in
`controllers`) use `httptest` servers and also need local port binding; they
fail only in the sandbox, not in CI.

## Docker Compose stack (phase 4 #12)

Already verified: `docker compose config` parses both the base file and the
base+proxy overlay; `config.example.json` is valid JSON and loads in gophish
(it starts, connects to SQLite, and runs all migrations, failing only at the
sandbox-blocked port bind).

Still to run (needs Docker):

```bash
cp .env.example .env      # edit the passwords
docker compose up -d --build
```

- Browse https://localhost:3333, accept the self-signed cert, log in as `admin`
  with `GOPHISH_INITIAL_ADMIN_PASSWORD`.
- Confirm MySQL is in use: data survives `docker compose restart`, and the
  `gophish` database has tables.
- Reverse-proxy overlay: `docker compose -f docker-compose.yml -f docker-compose.proxy.yml up -d --build`,
  then confirm the admin UI is reachable through Caddy over HTTPS with
  gophish's own `use_tls` off.
