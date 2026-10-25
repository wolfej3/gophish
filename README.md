![gophish logo](https://raw.github.com/gophish/gophish/master/static/images/gophish_purple.png)

Gophish
=======

![Build Status](https://github.com/wolfej3/gophish/workflows/CI/badge.svg)

Gophish: Open-Source Phishing Toolkit

[Gophish](https://getgophish.com) is an open-source phishing toolkit designed for businesses and penetration testers. It provides the ability to quickly and easily setup and execute phishing engagements and security awareness training.

### What's Changed

This version modernizes Gophish and adds admin security features. The details are in the sections below.

- **Security fixes:** stored XSS in the campaign results timeline (from visitors' User-Agent strings), locked accounts still working through API keys and existing sessions, HTML injection through Import Site, and a crash in Import Email. 71 known vulnerabilities in Go and dependencies are patched by moving to Go 1.26.
- **Data layer:** gorm v2 and pressly/goose replace the unmaintained gorm v1 and liamstask/goose. Existing databases keep their migration history and upgrade in place. The test suite runs against MySQL as well as SQLite in CI.
- **Single binary:** templates, admin assets, migrations, and the version are embedded, so Gophish runs from any directory. Building the admin assets only needs Go, not Node.js.
- **Sessions:** admin sessions are stored in the database, survive restarts, and are revoked on logout, password change, and account lock.
- **Admin security:** two-factor authentication, single sign-on through OIDC or SAML, an audit log, and API keys with limited permissions.
- **Teams and tenants:** objects are shared within teams instead of per login, teams nest under isolated client **tenants**, and a provider-admin role spans every tenant. Each user also gets a private Personal team. See [Teams](#teams) and [Tenants](#tenants).

#### Upgrading

Back up your database first. Migrations run automatically when Gophish starts. Then note:

- Everyone needs to log in again once, since sessions from the previous version aren't recognized.
- The admin interface must be reached over HTTPS (see [Admin Server TLS](#admin-server-tls)). Over plain HTTP, every login is refused.
- The `migrations_prefix` setting is no longer used, since migrations are embedded. It can be left in `config.json` or removed.
- MySQL needs zero dates allowed (see [Database](#database)).
- Server-side fetches (Import Site, webhooks) now block all internal, loopback, link-local, and cloud-metadata addresses by default. If you need to reach internal hosts, list them in `admin_server.allowed_internal_hosts`. Previously only link-local was blocked.
- - Deployments behind a reverse proxy must set `trusted_proxies` (see [Reverse proxies and client IPs](#reverse-proxies-and-client-ips)), or every client will appear to come from the proxy's IP. Previously Gophish trusted forwarded-for headers from anyone.

### Install

Installation of Gophish is dead-simple - just download and extract the zip containing the [release for your system](https://github.com/wolfej3/gophish/releases/), and run the binary. Gophish has binary releases for Windows, Mac, and Linux platforms.

### Building From Source
**If you are building from source, please note that Gophish requires Go v1.26 or above!** With Go's default toolchain settings, `go build` downloads the exact Go release pinned in `go.mod` automatically.

To build Gophish from source, simply run ```git clone https://github.com/wolfej3/gophish.git``` and ```cd``` into the project source directory. Then, run ```go build```. After this, you should have a binary called ```gophish``` in the current directory.

The admin interface's HTML templates, CSS, JavaScript, fonts, and images, along with the database migrations, are embedded in the binary at build time. If you change any of them, rebuild with `go build` to see the change. If you change the JavaScript or CSS sources, run `go run ./tools/assets` first so the bundles in `static/js/dist` and `static/css/dist` are regenerated before they're embedded, and commit the regenerated bundles along with your change. Building the assets only needs Go; Node.js isn't required.

Two things are still read from disk, relative to the directory Gophish runs in:

- `static/endpoint/`: files you want the phishing server to host.
- `static/db/geolite2-city.mmdb`: the GeoIP database used to map recipients' IP addresses to locations. If it's missing, Gophish still records events but skips the location lookup.

### Running the Tests

`go test ./...` runs the test suite against an in-memory SQLite database. To run it against MySQL, point `GOPHISH_TEST_MYSQL_DSN` at a database Gophish can drop and recreate, and run the packages one at a time, since they share it:

```
GOPHISH_TEST_MYSQL_DSN='root:password@(127.0.0.1:3306)/gophish_test?charset=utf8&parseTime=True&loc=UTC' go test -p 1 ./...
```

The MySQL server needs the `sql_mode` described under [Database](#database).

### Docker
This is a fork with additional security and feature changes (see [Security](#security)). The upstream `gophish/gophish` Docker image does not include these changes; build this fork's image yourself from the included `Dockerfile` (`docker build -t gophish .`). The Docker build compiles the admin assets with Go, so it no longer needs a Node.js stage.

### Setup
After running the Gophish binary, open an Internet browser to https://localhost:3333 and login with the default username and password listed in the log output.
e.g.
```
time="2020-07-29T01:24:08Z" level=info msg="Please login with the username admin and the password 4304d5255378177d"
```

Releases of Gophish prior to v0.10.1 have a default username of `admin` and password of `gophish`.

### Database

Gophish uses SQLite by default, storing its data in the file named by `db_path` (`gophish.db`). To use MySQL instead, set `db_name` to `mysql` and `db_path` to a connection string:

```json
"db_name": "mysql",
"db_path": "gophish:password@(127.0.0.1:3306)/gophish?charset=utf8&parseTime=True&loc=UTC",
```

Gophish stores zero dates, which MySQL's default strict mode rejects. Remove `NO_ZERO_DATE` and `NO_ZERO_IN_DATE` from the server's `sql_mode`, for example in `my.cnf`:

```
[mysqld]
sql_mode=ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION
```

Gophish is tested with MySQL 8.4.

### Admin Server TLS

The admin interface must be reached over HTTPS. Its CSRF protection rejects logins and other form submissions from plain `http://` pages, so an admin server reached over plain HTTP will show the login page but refuse every login with a `403 Forbidden` error.

There are two supported setups:

- **Built-in TLS (the default).** Keep `"use_tls": true` in the `admin_server` section of `config.json`. Gophish generates a self-signed certificate on first run, or you can point `cert_path` and `key_path` at your own certificate.
- **A TLS-terminating reverse proxy.** Set `"use_tls": false` and put a proxy such as nginx or Caddy in front of Gophish, serving the admin interface to browsers over HTTPS. Gophish logs a warning at startup when TLS is off as a reminder that it must sit behind such a proxy.

If you access the admin interface through a different hostname than the one the request arrives with (for example, a proxy that rewrites the `Host` header), add the browser-facing host to `trusted_origins`. Entries are host names with an optional port and no scheme, e.g. `["gophish.example.com"]` or `["gophish.example.com:8443"]`. Only list hosts you control: the CSRF library doesn't check the scheme of trusted origins ([GO-2025-3884](https://pkg.go.dev/vuln/GO-2025-3884), no upstream fix yet), so each entry should only ever serve Gophish over HTTPS.

In Docker, the admin server uses built-in TLS unless `ADMIN_USE_TLS=false` is set.

This applies only to the admin server. The phishing server's `use_tls` setting is unaffected.

### Reverse proxies and client IPs

Gophish uses the client's IP address to rate-limit logins and to record who did what in the audit log and in campaign results. Behind a reverse proxy the real client IP arrives in an `X-Forwarded-For` / `X-Real-IP` header, but any client can send those headers, so Gophish only trusts them from proxies you list.

Set `trusted_proxies` on `admin_server` and/or `phish_server` to the proxies' addresses as CIDRs (bare IPs are allowed and treated as `/32` or `/128`):

```json
"admin_server": {
	"trusted_proxies": ["10.0.0.0/8", "127.0.0.1"]
}
```

With `trusted_proxies` empty (the default), forwarded-for headers are ignored and the direct peer is always used, so no one can spoof their IP. A bad CIDR makes Gophish exit at startup with a clear error. In Docker, set `ADMIN_TRUSTED_PROXIES` / `PHISH_TRUSTED_PROXIES` to a comma-separated list.

Gophish reads the **first** entry of `X-Forwarded-For`. With nginx, set `proxy_set_header X-Forwarded-For $remote_addr;` (not `$proxy_add_x_forwarded_for`, which lets a client prepend a forged address).

### Teams

Campaigns, templates, landing pages, groups, and sending profiles belong to a **team** rather than to one user, so everyone on a team shares the same objects without sharing a login. Each object also records which user created it.

A user has a role on each team they belong to: **editor** (create and change the team's objects) or **viewer** (read only). A tenant admin can see and edit every team in their own tenant; a provider admin can see and edit every team in every tenant (see **Tenants** below).

Admins manage teams on the **Teams** page: create and rename teams, and add, remove, or change the role of each member. Users who belong to more than one team get a team switcher at the top of the sidebar, which decides the team the pages act in. Each user can also pick a **default team** (the one they land in when they sign in) from their Account Settings.

Every user also gets a private **Personal** team of their own, for solo work teammates can't see. You can't add other members to a personal team. Admins keep oversight: a tenant admin sees every personal team in their tenant and a provider admin sees them all, but one regular user never sees another's.

When you upgrade, everything that existed before teams moves into a team called **Default**, and every existing user becomes an editor on it, so nothing changes until you create more teams. Each user also gains a Personal team. A new user belongs to no shared team until an admin adds them to one.

### Tenants

Tenants let you host Gophish for several **separate client organizations** on one install, fully isolated from each other. A tenant contains its own teams and users; teams nest under a tenant, and every user belongs to exactly one tenant.

There are three global roles:

- **Provider admin** — the hosting operator. Spans every tenant, manages the tenants themselves, and can act on all teams and objects. Create and manage tenants on the **Tenants** page (provider admins only), and use the tenant switcher in the sidebar to scope the team and user lists to one tenant.
- **Admin** — a tenant administrator. Full control over teams, users, and objects, but only within their own tenant.
- **User** — works in the teams they belong to within their tenant.

When you upgrade, everything moves into a single **Default** tenant and the bootstrap admin account becomes the provider admin; every other existing admin becomes a tenant admin of Default. Creating a tenant seeds it with a shared default team so new users have somewhere to land.

A few things are intentionally out of scope: single sign-on, sending transports, and directory connectors stay operator-level configuration in `config.json`, shared across tenants (SSO-provisioned users join the Default tenant); usernames and team names are unique across the whole install, not per tenant.

### Sending through Microsoft Graph or Gmail

A sending profile's **interface type** chooses how it sends, for environments where SMTP auth is turned off:

- **SMTP** (the default) — unchanged.
- **Microsoft Graph** — sends through the Microsoft Graph API with an Entra ID app registration. The app needs the `Mail.Send` **application** permission (admin-consented). Fill in the directory (tenant) ID, application (client) ID, and client secret; the **Send as** address is the mailbox the mail is sent from (the app must be allowed to send as it).
- **Gmail** — sends through the Gmail API with a Google Workspace service account that has domain-wide delegation for the `gmail.send` scope. Give the path to the service account credentials file; the **Send as** address is the Workspace user to send as.

For both API transports the message is the same one the SMTP path builds, so templates, tracking, and the rest of a campaign work unchanged. Their credentials are stored on the sending profile, like the existing SMTP password. Secrets (SMTP/IMAP passwords, the Graph client secret) are never returned in API responses — they show as a placeholder you can leave unchanged to keep the stored value.

### People

The **People** page shows everyone who has been in a campaign in the current team and how they've responded across all of them, rather than one campaign at a time. For each recipient it shows how many campaigns they were in and in how many they opened the email, clicked, submitted data, or reported it. The counts are per campaign, so clicking twice in one campaign counts once. Opening a person shows their full history — the furthest status reached in each campaign — and a single timeline of their events across every campaign.

The same data is available from `GET /api/people/` (the list) and `GET /api/people/{email}` (one person's history and timeline), scoped to the team the request acts in.

### Directory Sync

A group's members can be pulled from an identity directory instead of, or alongside, the CSV import. You configure one or more **directory connectors** in `config.json`, and each group can point at a connector and a directory group, then sync its members on demand from the group editor.

Only the connector credentials live in `config.json`; the database stores just which connector and directory group each Gophish group uses, plus the last sync result. Syncing replaces the group's members with the directory's, keeping anyone you also added by hand in the same group only if they're in the directory. An empty directory result fails the sync rather than emptying the group, so a misconfiguration can't wipe a campaign's recipients. Editing a group's members by hand and importing a CSV both still work.

Connectors go under `admin_server`:

```json
"directory": [
	{
		"name": "Entra",
		"provider": "msgraph",
		"tenant_id": "...",
		"client_id": "...",
		"client_secret": "..."
	},
	{
		"name": "Workspace",
		"provider": "google",
		"credentials_file": "/etc/gophish/google-sa.json",
		"delegated_subject": "admin@example.com"
	},
	{
		"name": "AD",
		"provider": "ldap",
		"url": "ldaps://dc.example.com:636",
		"bind_dn": "CN=gophish,OU=Service,DC=example,DC=com",
		"bind_password": "...",
		"base_dn": "DC=example,DC=com"
	}
]
```

**Providers and the directory group identifier (`external_id`) each expects:**

- **`msgraph` (Microsoft Entra ID):** an app registration with the `GroupMember.Read.All` and `User.Read.All` application permissions (admin-consented). The directory group is its **object ID**.
- **`google` (Google Workspace):** a service account with domain-wide delegation for the `admin.directory.group.member.readonly` and `admin.directory.user.readonly` scopes, and `delegated_subject` set to an admin to act as. The directory group is its **email address**.
- **`ldap` (any LDAP directory, including Active Directory):** the defaults match Active Directory — members are found with `(&(objectClass=user)(memberOf=%s))` and read from `mail`, `givenName`, and `sn`. The directory group is its **distinguished name**. Override `user_filter`, `email_attribute`, `first_name_attribute`, and `last_name_attribute` for other LDAP schemas; `start_tls` and `insecure_tls` control the connection. Gophish refuses a plaintext `ldap://` connection unless you set `start_tls` or `insecure_tls`, and warns at startup when `insecure_tls` is used.

Setting a source or syncing takes the editor role on the group's team. Directory sync runs when you click **Sync Now**; scheduled syncing will come with the scheduling feature.

### Admin Security

#### Sessions

Admin sessions are stored in the database, so restarting Gophish doesn't log anyone out. The session cookie holds only a random token, and the database stores a hash of it. Sessions last five days. Logging out ends the session. Changing a password signs the user out of their other sessions, and locking or deleting an account signs it out everywhere. Logging in always starts a new session.

#### Two-factor authentication

Each user can turn on two-factor authentication under **Account Settings**. They scan a QR code with an authenticator app, confirm with a code, and get ten single-use recovery codes. From then on, signing in takes a code after the password (or after single sign-on). Turning it off or getting new recovery codes takes the password and a current code.

An admin can turn off two-factor authentication for a user who has lost their authenticator app and recovery codes, from the user's entry on the **User Management** page.

#### Single sign-on

Gophish can sign users in through an OpenID Connect (OIDC) or SAML 2.0 identity provider, such as Okta, Microsoft Entra ID, or Google Workspace. Each method adds a button to the login page.

The first time someone signs in, Gophish links their identity to the Gophish account whose username matches their email address (or another claim or attribute you choose). After that, the account follows the identity, even if the email address changes. If no account matches, sign in is refused, unless `auto_create_users` is on, in which case an account is created with `default_role` (`user` or `admin`, `user` by default). Auto-creating admins is refused unless you also set `allow_admin_auto_create`, and Gophish warns at startup if `auto_create_users` is on with no `allowed_domains`. `allowed_domains` limits sign in to email addresses in those domains. Locked accounts can't sign in, and users with two-factor authentication on still enter a code.

`disable_password_login` hides the password form and refuses password logins, so everyone signs in through the identity provider. API keys still work. If the identity provider becomes unavailable, remove the setting and restart Gophish to sign in with a password.

For OIDC, register Gophish with your identity provider using the redirect URL `https://<your Gophish address>/login/oidc/callback`, then add an `oidc` section to `admin_server` in `config.json`:

```json
"oidc": {
	"issuer": "https://login.example.com",
	"client_id": "gophish",
	"client_secret": "...",
	"redirect_url": "https://gophish.example.com:3333/login/oidc/callback",
	"button_label": "Sign in with Example",
	"auto_create_users": false,
	"allowed_domains": ["example.com"]
}
```

`scopes` (default `["email", "profile"]`) and `username_claim` (default `email`) can also be set. An email address the identity provider marks as unverified is refused.

For SAML, add a `saml` section to `admin_server`:

```json
"saml": {
	"idp_metadata_url": "https://login.example.com/app/gophish/sso/saml/metadata",
	"root_url": "https://gophish.example.com:3333",
	"button_label": "Sign in with Example",
	"username_attribute": "email"
}
```

Then give your identity provider Gophish's metadata from `https://<root_url>/saml/metadata`, or set it up by hand with the assertion consumer service URL `https://<root_url>/saml/acs` and the entity ID `https://<root_url>/saml/metadata`. Use a persistent NameID (or the user's email address), since accounts are linked to it. Without `username_attribute`, the NameID is matched against usernames. `idp_metadata_path` can point to a metadata file instead of a URL. Gophish signs its requests with the key pair in `cert_path` and `key_path` (default `gophish_saml.crt` and `gophish_saml.key`), which it creates if they don't exist. The identity provider's metadata is loaded once, on the first sign in, so restart Gophish after the identity provider changes its signing certificate. Sign in must start from Gophish; sign ins started from the identity provider's dashboard are refused.

An admin can unlink a user's single sign-on identity from the **User Management** page, so that the next identity to sign in with that username is linked instead.

#### Audit log

Gophish records logins and failed logins (with the reason and IP address), logouts, password and two-factor changes, impersonation, and every change made through the web interface or the API, including changes that were refused. When an admin impersonates another user, the entries note who is really acting. Admins can read the log on the **Audit Log** page, or from `GET /api/audit_log/`, which takes `username`, `action`, `limit` (up to 1000), and `before_id` (to page back) parameters.

#### Limited API keys

Each user's API key can do anything their account can. It's masked on **Account Settings**; revealing it takes re-entering your password (and a 2FA code if enabled), and it's no longer rendered into admin pages, so an XSS can't read it from the DOM. For scripts and integrations, users can also create limited keys there, or with `POST /api/api_keys/`. A limited key is read only or read and write, can be limited to some parts of the API (campaigns, groups, templates, landing pages, sending profiles, and so on), and can expire. Limited keys start with `gpk_`, are shown once when they're created, and are used the same way as the full key. A limited key can never do more than its owner's account, and it can't create or manage API keys. 

### Security

This fork addresses the following advisories against gophish v0.12.1, each with a regression test:

| Advisory | Issue | Fix |
|---|---|---|
| CVE-2026-107271 | Rate-limit bypass / IP spoofing via `X-Forwarded-For` | Proxy headers honored only from `trusted_proxies`; none trusted by default |
| CVE-2026-39904 | Zip-bomb attachments expand without limit | Per-entry/count/total caps; rejected at save time |
| CVE-2025-70963 | Full API key rendered into every admin page | Admin UI uses the session cookie + CSRF; key masked behind a password-gated Reveal |
| CVE-2026-107270 | Cross-team object access by ID | Verified 404 scoping across teams; creator preserved on update |
| CVE-2026-107273 | SSRF in Import Site | Internal ranges blocked at dial time by default; timeouts, body cap, redirect cap, scheme check |
| CVE-2026-82269 | API/session usable while password change required | Blocked for full keys, scoped keys, and sessions until changed |
| (README) | Limited API key could change its owner's password | Scoped keys can't write user accounts |
| CVE-2026-107272 | XSS via server/SMTP error messages | All flash/modal messages escaped |
| CVE-2024-2211 | XSS via campaign name | Escaped in dashboard chart tooltips (tables already escaped) |
| CVE-2026-107269 | Login timing username enumeration | Dummy bcrypt comparison on the no-such-user path |
| CVE-2022-45003 | DoS via autofocus in a cloned site | `autofocus` stripped on import |
| CVE-2022-45004 | XSS via a crafted landing page | Not applicable: landing-page HTML is never rendered in the admin origin |

Additional hardening: credentials are never returned in API responses (encryption at rest is a follow-up); SSO won't auto-create admins without an explicit flag; LDAP refuses plaintext connections without TLS; rendered templates are size-capped; and admin responses carry `nosniff`, `Referrer-Policy`, CSP `frame-ancestors`, and `X-Frame-Options`.

One upstream issue has no fix yet: [GO-2025-3884](https://pkg.go.dev/vuln/GO-2025-3884) in `gorilla/csrf` (`trusted_origins` ignores scheme), which only applies when `trusted_origins` is set — see [Admin Server TLS](#admin-server-tls).

### Documentation

Documentation can be found on our [site](http://getgophish.com/documentation). Find something missing? Let us know by filing an issue!

### Issues

Find a bug? Want more features? Find something missing in the documentation? Let us know! Please don't hesitate to [file an issue](https://github.com/wolfej3/gophish/issues/new) and we'll get right on it.

### License
```
Gophish - Open-Source Phishing Framework

The MIT License (MIT)

Copyright (c) 2013 - 2020 Jordan Wright

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software ("Gophish Community Edition") and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```
