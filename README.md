![gophish logo](https://raw.github.com/gophish/gophish/master/static/images/gophish_purple.png)

Gophish
=======

![Build Status](https://github.com/gophish/gophish/workflows/CI/badge.svg) [![GoDoc](https://godoc.org/github.com/gophish/gophish?status.svg)](https://godoc.org/github.com/gophish/gophish)

Gophish: Open-Source Phishing Toolkit

[Gophish](https://getgophish.com) is an open-source phishing toolkit designed for businesses and penetration testers. It provides the ability to quickly and easily setup and execute phishing engagements and security awareness training.

### Install

Installation of Gophish is dead-simple - just download and extract the zip containing the [release for your system](https://github.com/gophish/gophish/releases/), and run the binary. Gophish has binary releases for Windows, Mac, and Linux platforms.

### Building From Source
**If you are building from source, please note that Gophish requires Go v1.26 or above!** With Go's default toolchain settings, `go build` downloads the exact Go release pinned in `go.mod` automatically.

To build Gophish from source, simply run ```git clone https://github.com/gophish/gophish.git``` and ```cd``` into the project source directory. Then, run ```go build```. After this, you should have a binary called ```gophish``` in the current directory.

The admin interface's HTML templates, CSS, JavaScript, fonts, and images, along with the database migrations, are embedded in the binary at build time. If you change any of them, rebuild with `go build` to see the change. If you change the JavaScript or CSS sources, run `go run ./tools/assets` first so the bundles in `static/js/dist` and `static/css/dist` are regenerated before they're embedded, and commit the regenerated bundles along with your change. Building the assets only needs Go; Node.js isn't required.

Two things are still read from disk, relative to the directory Gophish runs in:

- `static/endpoint/`: files you want the phishing server to host.
- `static/db/geolite2-city.mmdb`: the GeoIP database used to map recipients' IP addresses to locations. If it's missing, Gophish still records events but skips the location lookup.

### Docker
You can also use Gophish via the official Docker container [here](https://hub.docker.com/r/gophish/gophish/).

### Setup
After running the Gophish binary, open an Internet browser to https://localhost:3333 and login with the default username and password listed in the log output.
e.g.
```
time="2020-07-29T01:24:08Z" level=info msg="Please login with the username admin and the password 4304d5255378177d"
```

Releases of Gophish prior to v0.10.1 have a default username of `admin` and password of `gophish`.

### Admin Server TLS

The admin interface must be reached over HTTPS. Its CSRF protection rejects logins and other form submissions from plain `http://` pages, so an admin server reached over plain HTTP will show the login page but refuse every login with a `403 Forbidden` error.

There are two supported setups:

- **Built-in TLS (the default).** Keep `"use_tls": true` in the `admin_server` section of `config.json`. Gophish generates a self-signed certificate on first run, or you can point `cert_path` and `key_path` at your own certificate.
- **A TLS-terminating reverse proxy.** Set `"use_tls": false` and put a proxy such as nginx or Caddy in front of Gophish, serving the admin interface to browsers over HTTPS. Gophish logs a warning at startup when TLS is off as a reminder that it must sit behind such a proxy.

If you access the admin interface through a different hostname than the one the request arrives with (for example, a proxy that rewrites the `Host` header), add the browser-facing host to `trusted_origins`. Entries are host names with an optional port and no scheme, e.g. `["gophish.example.com"]` or `["gophish.example.com:8443"]`. Only list hosts you control: the CSRF library doesn't check the scheme of trusted origins ([GO-2025-3884](https://pkg.go.dev/vuln/GO-2025-3884), no upstream fix yet), so each entry should only ever serve Gophish over HTTPS.

In Docker, the admin server uses built-in TLS unless `ADMIN_USE_TLS=false` is set.

This applies only to the admin server. The phishing server's `use_tls` setting is unaffected.

### Admin Security

#### Two-factor authentication

Each user can turn on two-factor authentication under **Account Settings**. They scan a QR code with an authenticator app, confirm with a code, and get ten single-use recovery codes. From then on, signing in takes a code after the password (or after single sign-on). Turning it off or getting new recovery codes takes the password and a current code.

An admin can turn off two-factor authentication for a user who has lost their authenticator app and recovery codes, from the user's entry on the **User Management** page.

#### Single sign-on

Gophish can sign users in through an OpenID Connect (OIDC) or SAML 2.0 identity provider, such as Okta, Microsoft Entra ID, or Google Workspace. Each method adds a button to the login page.

The first time someone signs in, Gophish links their identity to the Gophish account whose username matches their email address (or another claim or attribute you choose). After that, the account follows the identity, even if the email address changes. If no account matches, sign in is refused, unless `auto_create_users` is on, in which case an account is created with `default_role` (`user` or `admin`, `user` by default). `allowed_domains` limits sign in to email addresses in those domains. Locked accounts can't sign in, and users with two-factor authentication on still enter a code.

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

Each user's API key, shown on **Account Settings**, can do anything their account can. For scripts and integrations, users can also create limited keys there, or with `POST /api/api_keys/`. A limited key is read only or read and write, can be limited to some parts of the API (campaigns, groups, templates, landing pages, sending profiles, and so on), and can expire. Limited keys start with `gpk_`, are shown once when they're created, and are used the same way as the full key. A limited key can never do more than its owner's account, and it can't create or manage API keys. Note that a key with write access to users can change its owner's password.

### Documentation

Documentation can be found on our [site](http://getgophish.com/documentation). Find something missing? Let us know by filing an issue!

### Issues

Find a bug? Want more features? Find something missing in the documentation? Let us know! Please don't hesitate to [file an issue](https://github.com/gophish/gophish/issues/new) and we'll get right on it.

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
