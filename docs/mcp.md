# MCP: goarmin for AI assistants

goarmin speaks MCP, the [Model Context Protocol](https://modelcontextprotocol.io). An assistant
such as Claude can read everything goarmin knows about you and, with a token that allows it,
act for you: plan workouts, sync a watch, change settings, import files, add a watch. Back to
the [documentation index](README.md).

- **The endpoint** is `<public-url>/mcp` (Streamable HTTP), on the same port as the dashboard.
- **Access is by token.** Each account makes its own tokens under Settings › MCP. A token acts
  as that account and sees only its data, like the dashboard does.
- **Complete.** Every part of the app is there, except credentials (below).
- **Built on** the official Go SDK, `github.com/modelcontextprotocol/go-sdk`.

## Make a token

1. Open **Settings › MCP**.
2. Under **New token**, give it a name (one token per assistant or computer, so you can revoke
   each on its own), choose its **access** and when it **expires** (30 days, 90 days, a year or
   never).
3. **Make token.** goarmin shows the token once, with ready snippets to paste. It keeps only a
   fingerprint (SHA-256), so a lost token cannot be shown again: make a new one.

Tokens start with `gma_`. Treat one like a password.

| Access | What the assistant may do |
|---|---|
| **Read** | Read everything: health, sleep, training, the workout library, the calendar, devices, files, imports, account data |
| **Read and write** | Also plans, Sync now, watch and profile settings, preferences and the glance, imports, Bluetooth on, adding a watch; removals need the watch's name (below) |
| **Admin** (admins only) | Also people and invites, every device on the server, the running pairing, sync nodes |

A token whose level is too low does not even see the tools above it. Every call checks the level
again, and the token itself: not revoked, not expired, the account enabled, and an admin token's
account still an admin.

## Connect an assistant

**Claude Code** (one line, from the token's dialog):

```
claude mcp add --transport http goarmin https://goarmin.example/mcp --header "Authorization: Bearer gma_…"
```

**Any MCP client over HTTP** (the usual `mcpServers` JSON):

```json
{
  "mcpServers": {
    "goarmin": {
      "type": "http",
      "url": "https://goarmin.example/mcp",
      "headers": { "Authorization": "Bearer gma_…" }
    }
  }
}
```

**Clients that only start local commands** use the stdio bridge, `goarmin mcp`, from the same
binary. It forwards everything to the server; the token comes from the environment, never the
command line (shell history and the process list would show it).

```json
{
  "mcpServers": {
    "goarmin": {
      "command": "goarmin",
      "args": ["mcp", "-server", "https://goarmin.example"],
      "env": { "GOARMIN_MCP_TOKEN": "gma_…" }
    }
  }
}
```

`GOARMIN_MCP_TOKEN=gma_… goarmin mcp -server URL -list` prints what the server offers that
token. `-server` takes the server's address with or without `/mcp`, or `GOARMIN_MCP_SERVER`.
The bridge connects again by itself after the server ended an idle session (30 minutes).

Then ask, for example: "How did I sleep last night?", "Review my week", "Plan four easy runs
for next week from my library".

## What it offers

About a hundred tools, a few resources and four prompts. Settings › MCP lists all of them, by
area and access level.

- **Tools,** by area:
  - **Account:** profile, settings (preferences, the glance layout), the watch's profile,
    heart-rate zones.
  - **Health:** daily summaries, time series (heart rate, stress, Body Battery, …), nights and a
    night's stages, naps, body and weigh-ins, ECG, physiology, health status, hydration,
    snapshots.
  - **Training:** activities with laps, zones and tracks, lifetime totals, Garmin Coach,
    records, gear, golf clubs, the workout library (make, edit, rename and send workouts),
    the exercise catalogue, courses.
  - **Calendar:** the calendar; plan, move and remove workouts.
  - **Devices:** devices, Sync now, settings, sync logs, files on the watch, solar, settings
    history, live readings, Bluetooth, sync nodes; removals.
  - **Imports:** imports, uploading files (base64, at most 32 MB per call; larger ones in the
    dashboard), a watch on USB.
  - **Pairing:** adding a watch, step by step.
  - **Files:** stored files, the FIT inspector, account records from the Garmin Connect export.
  - **Admin:** users, invites, every device, the running pairing, sync nodes.
- **Answers** are what the dashboard gets from the API, as JSON. A few are trimmed for an
  assistant: tracks are thinned to `max_points` (600), the exercise catalogue is filtered by
  category or name, and an exercise comes without its figure's pose data. An answer over 1 MB is
  refused with a hint to ask for a shorter period.
- **Live readings** (`get_live_readings`) open the watch's live link, read for up to 30 seconds
  from the first value and return the readings. Opening takes about 10 seconds and holds the
  watch's Bluetooth. There is nothing to stop afterwards: the link closes by itself 30 seconds
  after the read (a call within them reuses it), so scheduled syncs go on.
- **Adding a watch** works as in the wizard: `start_pairing`, `pairing_scan`, then
  `get_pairing_session` every few seconds, `pairing_select`, and `pairing_confirm` only after
  you compared the code on the watch with the session's (you confirm on the watch too), then
  `pairing_setup`.
- **Resources** (files, read by URI):

  | URI | |
  |---|---|
  | `goarmin://activities/{id}/fit` | an activity's original FIT file |
  | `goarmin://courses/{id}/fit` | a course as FIT |
  | `goarmin://workouts/{id}/fit` | a library workout as the FIT file goarmin sends a watch |
  | `goarmin://files/{id}` | any stored raw file |
  | `goarmin://devices/{id}/raw.zip` | a device's raw files, zipped |
  | `goarmin://exercises/{category}/{number}` | an exercise with its figure's key poses |

  Files over 16 MB are downloaded in the dashboard instead.
- **Prompts** (slash commands in many clients): `review_week`, `review_night`, `plan_week`
  (it plans only after asking you, and only suggests with a read token), `explain_day`.

## Safety

- **Your data only.** A call runs the API's own handler as the token's account: the same
  validation, ownership and privacy rules as the dashboard. Another account's ids answer "not
  found". Admins still see nobody else's health data.
- **Credentials stay in the dashboard.** Signing in, the password, passkeys, sessions, the
  first-run setup, accepting invites and the MCP tokens themselves have no tools, and the
  server refuses those routes to MCP whoever asks. The node API (sync nodes) is not MCP either.
- **Removals name the watch.** `delete_device_data`, `delete_watch_files`, `unpair_device`,
  `hand_over_device`, the admin's release and forget, and `update_device` with
  `after_fetch: archive_delete` need `confirm` set to the watch's name (nickname, model or name),
  as the dashboard asks you to type it. A wrong name changes nothing. The assistant should ask
  you for it, never guess it.
- **Limits per token:** 120 calls a minute, 10 removals a minute.
- **The call log** under Settings › MCP shows each call's tool, token, time, result and
  duration, never its arguments. It is kept 30 days.
- **Revoking** a token ends it at once: its next request is refused.
- **Pages from other sites** cannot use the endpoint (cross-origin protection), and a request
  that reaches goarmin over loopback must name localhost or the public host (DNS rebinding).
- **Plain HTTP** carries the token in clear. That is fine on a private network such as a VPN
  mesh, as for sync nodes; anywhere else, put HTTPS in front ([running.md](running.md)).

## Turn it off

`goarmin serve -no-mcp` (or `GOARMIN_MCP=0`) turns the endpoint off: `/mcp` answers 404, no
token can be made, and Settings › MCP says so. Existing tokens are kept and work again once MCP
is back on.

## For developers

- **Code:** `internal/mcpserver`. The tool table is `Tools` (`tools.go`): each row names an API
  route, its arguments (path, query, body, or the tool's own), its level and hints. A call
  becomes an in-process request to the API (`api.Server.ServeAs`) with the token's account in
  the request context.
- **Complete by test.** `TestMCPCoversEveryRoute` (in `internal/api`) walks every route the API
  registers: each must be a tool or resource, or in `mcpserver.Excluded` with a reason. A new
  route without either fails `make check`, as does a tool that names a route the API does not
  have.
- **Adding a route** therefore means adding a row to `Tools` (or `Resources`), or an exclusion
  with its reason.
- **Tokens and the log:** `internal/store/mcp.go` (migration 19: `mcp_tokens`, `mcp_calls`);
  the dashboard's token routes are in `internal/api/mcp.go` (`docs/api.md`, *MCP*).
- **The bridge:** `mcpserver.Bridge`, used by `goarmin mcp`.
