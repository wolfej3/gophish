# Future features

A living backlog of features and enhancements that have been discussed and
intentionally deferred, with enough context to pick each up later. Nothing here
is committed work — each needs a go-ahead before it's built. For checks that
still need to run on already-merged work, see `VERIFICATION_PENDING.md`.

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

### MFA / 2FA lures
Simulate real-time credential-plus-OTP capture (e.g. an attacker-in-the-middle
style flow) rather than only static credential capture. Deferred from phase 4.

### Follow-up training
After a target interacts with a lure, deliver a short training page/module and
record completion, so a campaign can both test and educate. Deferred from
phase 4.

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
