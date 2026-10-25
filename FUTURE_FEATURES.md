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
