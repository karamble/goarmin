# Security

goarmin holds health data. If you find a way to read someone else's data, sign in without the
right credentials, or make goarmin send data where it shouldn't, please report it privately.

## Reporting

E-mail **hello@decredcommunity.org** with a description and, if you can, the steps to reproduce.
Please don't open a public issue for a vulnerability. You'll get an answer, and a fix is
credited to you if you like.

## What goarmin stores and exposes

- **On disk:** everything stays in the data directory: a SQLite database and the raw files
  from your watches and imports. Backups hold the same; treat them like the data.
- **Accounts:**
  - passwords are argon2id;
  - sessions and invitations are stored as SHA-256 hashes of their tokens;
  - passkeys use WebAuthn, bound to the configured public address.
- **Isolation:** every data route is scoped to the signed-in account, and another account's ids
  answer 404. Admins manage accounts but see no one's health data.
- **HTTP:**
  - same-origin writes only;
  - a strict Content Security Policy (`default-src 'self'`);
  - HttpOnly session cookies, Secure on HTTPS.
- **Outbound:** goarmin itself only fetches map tiles, and only when a tile server is configured.
  Tiles are served to signed-in users only, so the server is no open proxy. The browser loads
  nothing from third parties.
- **The watch:** goarmin answers the watch's position requests with "location services off" and
  refuses its web requests.

## Scope

In scope: the goarmin server, the dashboard, the CLI, and the Docker image as shipped.

Out of scope:
- vulnerabilities in a reverse proxy, the host or BlueZ;
- attacks that need the user's own unlocked computer;
- Garmin's firmware.
