# Future features

A living backlog of features and enhancements that have been discussed and
intentionally deferred, with enough context to pick each up later. Most items
here need a go-ahead before they're built. For checks that still need to run on
already-merged work, see `VERIFICATION_PENDING.md`.

**Active roadmap (set 2026-10-30).** Build order: **(1) Gmail DMI — shipped,
(2) MCP server endpoint, (3) credential encryption at rest.** Also back on the
roadmap after being deferred from phase 4: **MFA / 2FA lures** and **follow-up
training** (the training design will be driven by the product owner — don't
design it ahead of their input). Postgres remains out of scope (tenants are
built on MySQL).

## Lures

### Printable / per-location tracked QR codes
A physical-engagement companion to the email QR lures (`{{.QR}}`). **Not** a
plain "download a QR of my landing URL" button — that adds little over free
online generators, and Gophish can't track a scan with no recipient id
(`controllers/phish.go` rejects a landing request whose `rid` is empty), so a
generic printed QR records nothing.

The version worth building: **per-location tracked codes** — generate a batch of
unique tracked URLs, each its own code/pseudo-target the operator labels by
placement ("Lobby poster", "Garage L2"), rendered as downloadable QR PNGs. This
reuses the existing per-recipient tracking, so each printed code gets location
attribution (which poster was scanned, and when). A fully general
"anonymous walk-up scans / aggregate counts" variant would need a new,
non-per-recipient tracking concept and is a larger, separate effort.

Recommendation: build only if physical social-engineering engagements become a
real use case; treat it as its own small feature, not a bolt-on to the email QR.

### SMS lures — convincing fake link preview
The SMS feature serves preview/scanner bots a **blank, no-Open-Graph** response
so no revealing card renders and no premature click is recorded. A follow-up
could instead serve a **controlled, convincing preview card** (operator-chosen
Open Graph title/description/image) to increase realism, while still not
recording the bot fetch as a human click.

### SMS lures — dedicated short domain for link masking
Masked links are served from the campaign's own phishing domain today
(`https://host/<code>`). A follow-up could add a configurable **separate short
domain** (`https://a-co.me/<code>`) that redirects to the landing page, matching
how real orgs text branded shortened links. The code→recipient mapping is
unchanged; only the host used to build the link differs, plus its own TLS and
DNS.

### SMS lures — phone-only targets
Today a group target still requires a valid email address even if the campaign
only sends SMS. Supporting **phone-only** targets (no email) means relaxing
target validation and changing campaign recipient de-duplication to key on the
phone number when no email is present, plus making the email column optional in
the group UI/CSV. Deferred to keep the first SMS release low-risk.

### SMS lures — additional providers
The SMS channel ships with Twilio behind a small provider interface. Additional
providers (Vonage/Nexmo, AWS SNS, MessageBird, Telnyx, …) can be added as new
implementations of that interface without touching the send pipeline.

### Gmail direct inbox injection (DMI) — SHIPPED 2026-10-30

**Done.** Model field `gmail_insert` on `SMTP` + dual migration
`20261107000000_0.16.0_gmail_insert.sql`; `GmailConfig.Insert` and the
`gmailInsertSender` (`messages.insert`, `gmail.insert` scope, per-recipient
delegated subject, `labelIds: ["INBOX","UNREAD"]`) in `sender/gmail.go`;
`GetDialer` wiring; a "Deliver by direct inbox injection (DMI)" checkbox on the
Gmail sending profile; `TestGmailInsert`. Documented in the README under
*Gmail direct inbox injection (DMI)*. The original design is kept below for
reference.

Confirmed with the user: "DMI" = **direct mailbox injection** — for authorized
internal Google Workspace simulations, drop the lure straight into the target's
Gmail inbox via the Gmail API instead of delivering over SMTP/`messages.send`. It
bypasses spam/filtering (so an internal test isn't skewed by the org's own
gateways and delivery is guaranteed). Built as an **alternative delivery mode on
the existing Gmail sending profile**, not a new transport. Planned in detail,
then paused.

**Key difference from the current Gmail sender** (`sender/gmail.go`): today's
sender calls `users/me/messages/send` and, via domain-wide delegation,
impersonates the **sender** (`GmailConfig.From`, baked into the client once at
dial time). DMI uses `users/me/messages/insert`, which writes into a mailbox
without delivering — so it must impersonate the **recipient**, and the recipient
differs per message. Consequences:
- Different scope: `https://www.googleapis.com/auth/gmail.insert` (not
  `gmail.send`).
- The service account's domain-wide delegation must cover the **recipients'**
  mailboxes (same Workspace) for that scope. `FromAddress` is just the lure's
  From header in insert mode (no delegation needed over it).
- The delegated subject can't be fixed at dial time; the sender mints a
  per-recipient client at send time.

**Design:**
- *Model + migration:* add `GmailInsert bool json:"gmail_insert" gorm:"column:gmail_insert"`
  to `SMTP` (`models/smtp.go`); new dual migration (next timestamp after
  `20261106000000_0.15.0_sms_lures.sql`, e.g. `20261107000000_0.16.0_gmail_insert.sql`)
  `ALTER TABLE smtp ADD COLUMN gmail_insert boolean NOT NULL DEFAULT 0;`
  (dialect idioms: backticks vs quotes). No new validation (a Gmail DMI profile
  still just needs `credentials_file` + `FromAddress`).
- *Dialer wiring:* `GetDialer()` `GmailInterface` case passes `Insert: s.GmailInsert`
  into `sender.GmailConfig`.
- *Sender (`sender/gmail.go`):* add `Insert bool` to `GmailConfig` and
  `const gmailInsertScope`. In `NewGmailDialer`, pick the scope by mode. Send
  mode = unchanged (`Subject = From`, `gmailSender`). Insert mode returns a
  `gmailInsertSender` holding the parsed `*jwt.Config` (insert scope, empty
  Subject) + baseURL/TokenURL; its `Send(from, to, msg)` computes `mimeBytes`
  once, then for each recipient copies the jwt config with `Subject = recipient`,
  builds `cfg.Client(ctx)`, and POSTs `{baseURL}/users/me/messages/insert` with
  `{"raw": <base64url>, "labelIds": ["INBOX","UNREAD"]}`; non-2xx → error with
  the API message (mirror `messages.send`). Reuse `noopCloser`, `apiTimeout`,
  `mimeBytes`. QR/tracking already live in the MIME, so DMI inherits them.
- *UI:* a checkbox in the `.transport-gmail` block of
  `templates/sending_profiles.html` ("Deliver by direct inbox injection (DMI)")
  wired in `static/js/src/app/sending_profiles.js` (load/save/clear
  `profile.gmail_insert`; regenerate the bundle), with help text about the
  `gmail.insert` scope + recipient-mailbox delegation and that it's for
  authorized internal Workspace tests.
- *Docs:* extend the README "Sending through Microsoft Graph or Gmail" section.
- *Tests:* mirror `TestGmailSend` (stubs `TokenURL`+`BaseURL` with `httptest`;
  runs in CI, not the sandbox) asserting the request hits `…/messages/insert`,
  carries `labelIds ["INBOX","UNREAD"]` + the raw MIME, and mints a per-recipient
  client (recipient as the delegated subject).

**Recommendation / when to build:** self-contained and medium-sized; prerequisites
are a Workspace + a service account with domain-wide delegation for `gmail.insert`
over the test recipients. The full plan above can be executed as written.

### MFA / 2FA lures
Simulate real-time credential-plus-OTP capture (e.g. an attacker-in-the-middle
style flow) rather than only static credential capture. Deferred from phase 4,
**back on the active roadmap (2026-10-30)** — to be built after the MCP endpoint
and credential encryption at rest.

### Follow-up training
After a target interacts with a lure, deliver a short training page/module and
record completion, so a campaign can both test and educate. Deferred from
phase 4, **back on the active roadmap (2026-10-30)**. The product owner will
drive the training design and has significant ideas for this area, so hold on a
detailed design until that input lands.

## Security / maintenance

### Replace CKEditor 4 with a CodeMirror source editor + sandboxed preview
The template and landing-page HTML editors use a vendored **CKEditor 4.11.1**
(`static/js/src/vendor/ckeditor/`), which is end-of-life. The config sets
`fullPage = true` and `allowedContent = true` (`config.js`), and the editor
renders HTML imported from arbitrary external sites (`controllers/api/import.go`
ImportSite, plus email import) as editable content on the admin origin. Even the
last free release, **4.22.1**, is still exposed to CVE-2024-24815 (fullPage CDATA
XSS); that is fixed only in the commercial 4.24-lts line. Upgrading within
CKEditor 4 therefore does not close the exposure.

The editors already open in **source mode**, so the plan is to drop WYSIWYG
entirely:
- Edit HTML as text in **CodeMirror 6** (MIT) — `html()` language, line numbers,
  history, search, bracket matching, line wrapping.
- A **Preview** toggle renders the HTML in a fully sandboxed iframe (`sandbox=""`,
  `srcdoc`): no scripts, no forms, no navigation, opaque origin — stricter than
  the `allow-same-origin` preview already used in `template_library.js`.
- Keep the `{{.` autocomplete (the tag list currently in
  `static/js/src/app/autocomplete.js`) as a CodeMirror completion source, and keep
  the Insert-QR button (`insertText("{{.QR}}")`).

**No npm tooling returns.** CodeMirror ships as prebuilt ES modules; vendor each
package's `dist/index.js` + LICENSE under `static/js/src/vendor/codemirror/` and
bundle them with the existing Go esbuild tool via `vendorModules` in
`tools/assets/main.go` — exactly how `vendor/zxcvbn` + `app/passwords.js` work
today. Getting the files in needs a one-time download outside the build sandbox
(npm registry or release tarballs); versions get pinned in a `VERSIONS` file.
Packages: `@codemirror/{state,view,language,commands,autocomplete,search,
lang-html,lang-css,lang-javascript,lint}`,
`@lezer/{common,highlight,lr,html,css,javascript}`, `style-mod`, `w3c-keyname`,
`crelt`, `@marijn/find-cluster-break`.

Work involved:
- New `static/js/src/app/html_editor.js` (ES module →
  `dist/app/html_editor.min.js`) exposing
  `create`/`getValue`/`setValue`/`insertText`/`showSource`/`showPreview`; move
  `TEMPLATE_TAGS` into it and delete `app/autocomplete.js`. Fix the completion
  regex (`[A-z]` → `[A-Za-z]`, the CodeQL finding) along the way.
- Add `html_editor.js` to `moduleScripts` and its vendor packages to
  `vendorModules` in `tools/assets/main.go`; drop `autocomplete.js` from
  `appScripts`.
- Rework `static/js/src/app/templates.js` and `landing_pages.js`: create the
  editor once; replace `$("#html_editor").val()` /
  `CKEDITOR.instances[...].getData()` with the new API; import switches to Preview
  instead of `setMode('wysiwyg')`. Remove CKEditor-only code — the
  `https?://{{.URL}}` scheme fix, `CKEDITOR.on('dialogDefinition')`, and the
  `.cke` clause in the `enforceFocus` overrides (also in `sending_profiles.js`).
- Swap the script tags in `templates/templates.html` and `landing_pages.html`;
  give the editor a fixed height in `static/css/main.css`.
- Delete `static/js/src/vendor/ckeditor/` (also clears the CodeQL `wsc` alerts),
  run `go run ./tools/assets`, commit regenerated bundles, `go test ./...`.

**Behavior change to call out when built:** rich-text (WYSIWYG) editing and the
link/image/table dialogs go away — operators edit HTML and preview it, and saved
HTML is no longer normalized or wrapped in `<html><body>` by the editor. Since the
editors already default to source mode, the day-to-day impact is small.

**Alternative considered and rejected:** staying on CKEditor 4 (upgrade to
4.22.1). It leaves CVE-2024-24815 open because of `fullPage`, keeps an unmaintained
dependency, and the free line gets no further security fixes. CKEditor 5 /
TinyMCE 7 are GPL-or-commercial and handle full-page HTML poorly. CodeMirror
removes the XSS class rather than tracking CVEs.

Recommendation: worth doing — it's the actual fix for the editor's security
exposure, not just a version bump. Schedule it as its own change; the main cost is
the one-time vendoring of CodeMirror.

## AI / automation

### MCP server endpoint (`gophish mcp`)
Expose Gophish over the **Model Context Protocol** so AI agents/assistants
(Claude Desktop, etc.) can drive it — list and build campaigns, launch them, read
results — through a standard tool interface. This was phase 4 item 11; planned in
detail, then tabled before implementation.

**Decisions already made (with the user):**
- **Transport: a `gophish mcp` stdio subcommand**, not an HTTP endpoint on the
  admin server. It speaks MCP over stdio (how desktop AI clients connect) and
  **adapts to Gophish's existing REST API** over HTTP with a configured base URL
  + API key. Fully decoupled from the running server; no new network attack
  surface. (The official SDK also offers a Streamable-HTTP handler if a networked
  `/mcp` endpoint is ever wanted — a later option, not this.)
- **Full curated toolset, bounded by the API key.** Every tool is just a REST
  call carrying the caller's key, so the key's permissions/scope/team/tenant and
  the view-only rule are inherited verbatim — a read-only or scoped `gpk_` key
  yields a read-only/limited MCP with **zero extra enforcement code**. The API
  key *is* the permission model.

**Why the REST-adapter shape:** reusing the REST API (rather than calling
`models` in-process) means 100% reuse of auth, scoped-key limits
(`middleware.EnforceAPIKeyScope`), team/tenant resolution (`middleware/team.go`),
`EnforceViewOnly`, and request validation — nothing is reimplemented or at risk
of drifting. A stdio subcommand is a separate process from the server anyway, so
talking to it over HTTP is the natural boundary.

**Design:**
- *Dependency:* `github.com/modelcontextprotocol/go-sdk/mcp` (v1.8.0 at time of
  planning). Provides `mcp.NewServer`, `mcp.AddTool`, and
  `server.Run(ctx, &mcp.StdioTransport{})`. **Caveat:** adding a new module needs
  network for `go get`; in the restricted sandbox the module cache is read-only,
  so the dep must be fetched where the Go proxy is reachable.
- *Subcommand dispatch* in `gophish.go`: before kingpin parses, detect the
  subcommand so the server path is untouched —
  `if len(os.Args) > 1 && os.Args[1] == "mcp" { mcpserver.Run(os.Args[2:]); return }`.
  `gophish` with no args (and `--mode …`) behaves exactly as today. The MCP path
  does **not** load `config.json`, open the DB, or start servers; it's a client.
- *New package `mcpserver/`:*
  - `client.go` — thin REST client (base URL, bearer key, optional default team
    id, `insecure` TLS toggle). `Authorization: Bearer <key>`; team/tenant via
    `X-Gophish-Team-Id` / `X-Gophish-Tenant-Id`. Uses a configurable
    `http.Client` so tests inject a `RoundTripper`. `--insecure` sets
    `InsecureSkipVerify` for the self-signed admin cert.
  - `server.go` — `Run(args []string) error`: a dedicated `flag.FlagSet`
    (`--url` default `https://localhost:3333`, `--insecure`, `--team`), key from
    env `GOPHISH_MCP_API_KEY` (env, not a flag, to keep it out of the process
    list; error if unset), build the client, construct the `mcp.Server`, register
    tools, `server.Run(ctx, &mcp.StdioTransport{})`.
  - `tools.go` — the curated toolset with typed input structs (SDK generates the
    JSON schemas); each handler calls the REST client and returns the API JSON,
    mapping non-2xx to an MCP tool error carrying the API's message. Each tool
    takes an optional `team_id` to override the default team.
- *Tools (map to existing endpoints):*
  - Campaigns: `list_campaigns`, `get_campaign`, `get_campaign_results`,
    `get_campaign_summary`, `create_campaign` (creates & launches),
    `complete_campaign`, `delete_campaign`.
  - Building blocks (list/get + create where needed to assemble a campaign):
    templates, pages, groups, sending profiles.
  - Read/recon: `list_people`, `get_person`, `list_teams`, and `list_tenants`
    (succeeds only for a provider-admin key; otherwise surfaces the 403).

**Docs:** a README "MCP server" section — create a (optionally scoped/read-only)
API key in the UI, run
`GOPHISH_MCP_API_KEY=… gophish mcp --url https://host:3333 --insecure`, and a
sample Claude Desktop `mcpServers` config block. Emphasize the key is the
permission boundary, and `--insecure` is for the self-signed admin cert.

**Tests:** mock the REST client via a stub `http.RoundTripper` (no server/port,
so it runs in the sandbox and CI): assert the bearer/team headers, method/path/
body, non-2xx→error mapping, and that a couple of tool handlers pass the JSON
through. End-to-end (needs a running gophish + built binary): connect with the
MCP Inspector or Claude Desktop, list tools, call `list_campaigns`, then
`create_campaign`; verify a **read-only scoped key** makes write tools fail with
a permission error (proving the key is the boundary).

**Recommendation / when to build:** self-contained and low-risk — the only real
prerequisites are fetching the SDK dependency (network) and a running gophish to
point it at. Pick it up when agent-driven operation of the platform is actually
wanted; the full plan above can be executed largely as written.

## Scheduling

### Arbitrary time-zone picker
Scheduled launches currently use the operator's browser time zone (shown
explicitly) and are stored UTC. A true per-schedule time-zone picker needs
`moment-timezone` added to the vendor bundle.

### Reschedule a queued campaign
Let an admin edit the launch / send-by date of an already-queued one-off
campaign instead of deleting and recreating it. Requires regenerating every
result and maillog `SendDate` from the new dates.

## Tracking

### Configurable preview/scanner filter
The preview/scanner bot filter (which suppresses false clicks from link-preview
and email-security bots) ships default-on with a built-in denylist. A follow-up
could make it configurable — per-campaign toggle, or an editable denylist — for
operators who specifically want to see scanner activity.
