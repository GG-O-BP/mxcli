---
title: mxcli tunnel-hub — GitHub authentication (hosted hub.mxcli.org)
status: proposed
date: 2026-07-26
---

# Proposal: `mxcli tunnel-hub` — GitHub authentication for the hosted hub

**Status:** Proposed
**Date:** 2026-07-26
**Author:** Generated with Claude Code

This proposal adds authentication to the multi-tenant tunnel-hub so a **hosted**
instance (`hub.mxcli.org`) can (a) show each user only their **own** app previews
and (b) limit who may register a runtime. It is the security follow-up flagged in
[`PROPOSAL_mxcli_dev_warm_loop.md`](PROPOSAL_mxcli_dev_warm_loop.md) ("this version
uses one shared `--secret` and open registration … per-tenant auth is a follow-up")
and continues that document's slice numbering as **slice 6**.

**Self-hosted hubs stay open.** Everything here is opt-in via config; a hub started
without the GitHub flags behaves exactly as it does today.

## Decisions locked (this proposal)

- **Access model: owner-only.** A viewer sees a preview iff their GitHub login is the
  one that registered it. No repo/team sharing in this cut (revisitable — see
  *Future work*).
- **Identity mechanism: a single GitHub OAuth App.** Web flow for browsers, device
  flow for the headless CLI/agent. No GitHub App installation, no repo-access checks.

## Problem

Today (`cmd/mxcli/tunnelhub/`):

- **No viewer auth.** Anyone who knows a preview subdomain reaches the app; the admin
  overview at `hub.<domain>/` lists *everyone's* previews.
- **No owner identity.** `Backend` (registry.go) carries `Project/Branch/Prefix/...`
  but nothing about *who* owns it. `List`/`/api/backends` cannot filter per user.
- **Registration is a shared secret.** `/api/register` is gated (optionally) by one
  hub-wide `RegisterSecret` (`X-Hub-Secret`, from `--hub-secret`). Everyone who can
  register shares the same secret; the returned token is opaque and owner-less.

For a public `hub.mxcli.org` this is unacceptable: previews are world-readable and any
holder of the one secret can register. We need per-user isolation on both planes —
**who may view** a preview and **who may register** one — keyed to a GitHub identity
(every Claude Code web user already has a GitHub account and repo).

## Goals

- A viewer of `*.mxcli.org` must authenticate with GitHub and may reach only previews
  they own.
- A Claude Code web session (headless) can register a runtime with a **per-user**
  credential, not a shared secret.
- Single sign-on across preview subdomains (log in once, not per app).
- The GitHub OAuth token never leaves the trust boundary it belongs to (browser↔GitHub,
  or mxcli↔GitHub) — the hub sees only what it mints.
- **Zero behaviour change for self-hosted hubs** when the GitHub flags are absent.

## Non-goals (this cut)

- Repo-based or team sharing (owner-only for now).
- GitHub App installation flow / fine-grained repo permissions.
- Persisting the registry to disk (still in-memory; keys are the only new durable state
  — see *Open questions*).
- Authenticating the chisel tunnel per-user (still the shared `TunnelAuth`; the
  owner-scoped registration token remains the practical tunnel gate — see *Security*).

## Design overview: two planes, both keyed to GitHub

```
 ┌───────────── browser ─────────────┐         ┌──────── Claude Code web / CLI ────────┐
 │ GET app-xyz.mxcli.org             │         │ mxcli auth hub login  (device flow)   │
 │  └─ no cookie → 302 GitHub OAuth  │         │  └─ GitHub device code → hub mints    │
 │      web flow → callback on hub   │         │      an API key bound to the login    │
 │      → signed cookie .mxcli.org   │         │ mxcli run --hub  → X-Hub-Key: <key>   │
 │  └─ cookie.login == preview.Owner │         │  └─ hub: key → login → stamp Owner    │
 │      ? proxy : 403                │         └───────────────────────────────────────┘
 └───────────────────────────────────┘
```

### Plane 1 — Viewer (browser): GitHub OAuth web flow

New auth middleware wraps the front handler in `server.go` (`ServeHTTP`) for **preview
subdomains and the admin page**, and is skipped for the chisel WS control path and ACME
HTTP-01 (neither is a browser):

1. Request to `app-xyz.mxcli.org` with no valid session cookie → `302` to GitHub's
   `authorize` endpoint (`client_id`, `redirect_uri=https://hub.mxcli.org/auth/github/callback`,
   `scope=read:user`, signed `state` carrying the return URL).
2. GitHub redirects back to `hub.mxcli.org/auth/github/callback`; the hub exchanges the
   code for a GitHub token **server-side**, calls `GET /user` once to learn the login,
   then sets a **signed session cookie** and 302s back to the original preview URL.
3. Cookie is `Domain=.mxcli.org; Secure; HttpOnly; SameSite=Lax` so it is **single
   sign-on across every `*.mxcli.org` subdomain**. Payload: `{login, exp}`, signed
   (HMAC) with a hub secret; no GitHub token stored in the cookie or server-side.
4. On each preview request the middleware verifies the cookie and checks
   `cookie.login == backend.Owner`; mismatch → `403` (a small "not your preview" page);
   match → existing reverse-proxy path.

The callback lives on `hub.mxcli.org` (one registered OAuth callback URL) even though
the protected resource is a subdomain — the cross-subdomain cookie makes that work.

### Plane 2 — Registration (headless CLI/agent): hub-issued API key

GitHub does not hand out generic service API keys — it issues *identity* (OAuth tokens,
PATs). So the hub **mints its own key**, seeded once by a GitHub login, and
`run --hub` sends that key — never a GitHub token — on `/api/register`.

**Bootstrap (`mxcli auth hub login`), once per environment:**

1. mxcli starts GitHub's **device flow** (`POST /login/device/code`), prints
   `Go to https://github.com/login/device and enter ABCD-1234`.
2. User authorizes in any browser; mxcli polls `POST /login/oauth/access_token` until it
   gets a GitHub token. **This token stays local to mxcli.**
3. mxcli calls the hub `POST /api/keys` with `Authorization: Bearer <github-token>`. The
   hub validates it against `GET https://api.github.com/user`, learns the login, mints an
   opaque **hub API key** bound to that login, stores `key → login`, and returns the key.
4. mxcli caches the key in `~/.mxcli/auth.json` (the same file the Mendix marketplace PAT
   already uses, mode `0600`), keyed by hub host.

   *Trade-off:* step 3 briefly sends the GitHub token to the hub. Alternative that keeps
   the token entirely off the hub: run the whole OAuth on the hub (`auth hub login` opens
   a hub URL that does GitHub web-flow and displays a key to paste back). Slightly more
   friction; called out in *Open questions*.

**Register:** `run --hub` reads the cached key and sends `X-Hub-Key: <key>` on
`/api/register`. The hub resolves `key → login`, stamps `Backend.Owner = login`, and
returns the existing registration token. Heartbeat/deregister continue to use that
per-registration bearer token unchanged.

**Claude Code web:** the key must survive container reaping. Two supported paths:
- A **repo/environment secret** `MXCLI_HUB_KEY` the user sets once (read by `run --hub`
  before falling back to `~/.mxcli/auth.json`) — deterministic, best for web sessions.
- `mxcli auth hub login` on demand (device flow) — fine for a local dev machine.

## Data model changes

`Backend` (registry.go) gains one field:

```go
Owner string `json:"owner"` // GitHub login that registered it ("" = anonymous/self-hosted)
```

- `RegisterRequest` is unchanged on the wire; `Owner` is derived server-side from the
  `X-Hub-Key` → login lookup, never trusted from the client body.
- `identity()` gains `Owner` as its first component so two users' identically-named
  projects/branches never collide on one slot.
- `List(sortKey, viewerLogin string)` filters to `b.Owner == viewerLogin` when auth is
  on; `viewerLogin == ""` (self-hosted / auth off) returns all, preserving today's
  behaviour. `/api/backends` and the admin page pass the cookie login through.

New durable state: a `keys` store `map[string]string` (hub key → GitHub login). In-memory
for the first cut (see *Open questions* re: persistence).

## HTTP surface

| Method & path (on `hub.mxcli.org`) | Purpose |
|---|---|
| `GET /auth/github/login` | Begin web flow (302 to GitHub), signed `state` = return URL |
| `GET /auth/github/callback` | Exchange code, set `.mxcli.org` session cookie, 302 back |
| `POST /auth/logout` | Clear the session cookie |
| `POST /api/keys` | Mint a hub API key (auth: `Bearer <github-token>`) — bootstrap |
| `DELETE /api/keys` | Revoke the caller's key |
| `POST /api/register` | **now** authed by `X-Hub-Key` (was `X-Hub-Secret`) → stamps `Owner` |
| `GET /api/backends`, admin `/` | filtered to the viewer's `Owner` |

Preview subdomains: unchanged path, now behind the viewer-auth middleware.

## Config / flags (`mxcli tunnel-hub`)

```
--github-oauth-client-id     GitHub OAuth App client id     (enables viewer + key auth)
--github-oauth-client-secret GitHub OAuth App client secret (env: MXCLI_HUB_GH_SECRET)
--session-secret             HMAC key for the session cookie (env: MXCLI_HUB_SESSION_SECRET)
--require-auth               require a valid session for every preview + register (default: on
                             when a client id is set; forced off when it is not)
```

**Absent client id ⇒ open mode** — the middleware is a no-op, `/api/register` keeps
honouring the legacy `--hub-secret`, `List` returns everything. A self-hosted
`mxcli tunnel-hub --domain example.com` is byte-for-byte today's behaviour.

Client (`mxcli run --hub` / `mxcli auth`):
- `mxcli auth hub login [--hub https://hub.mxcli.org]` — device-flow bootstrap.
- `mxcli auth hub status` / `logout`.
- `run --hub` sends `X-Hub-Key` from `MXCLI_HUB_KEY` or `~/.mxcli/auth.json`; falls back
  to `--hub-secret` (`X-Hub-Secret`) for open self-hosted hubs.

## Security considerations

- **Cookie scope.** `Domain=.mxcli.org` is deliberate (SSO), so the signing key must be
  strong and rotatable; cookie carries only `{login, exp}`, HMAC-signed, short TTL with
  silent re-auth (the GitHub session makes the redirect invisible when still valid).
- **GitHub token containment.** Never stored in the cookie; only used transiently
  (browser callback exchange, or device-flow key mint) and discarded. `scope=read:user`
  only.
- **Tunnel plane.** The chisel control connection still uses the shared `TunnelAuth`; a
  cross-user request can't reach another app because routing is by owner-checked
  subdomain and the reverse port is server-allocated, not client-chosen. Per-user tunnel
  auth is deferred (non-goal) — the owner check on the front is the enforced boundary.
- **Key theft.** A leaked `X-Hub-Key` lets an attacker register previews *as that user*
  (annoyance/quota), not view others' apps. `DELETE /api/keys` + short-lived keys
  mitigate; rotating is cheap (re-run `auth hub login`).
- **Open-mode safety.** Because auth is gated on `--github-oauth-client-id`, a
  misconfigured hosted hub that forgets the secret fails **closed** only if
  `--require-auth` defaults on with a client id present; document that a client id
  without a session secret refuses to start.

## Implementation slices

1. **Owner field + filtered list** (no auth yet): add `Backend.Owner`,
   `List(sort, viewer)`, thread a viewer through admin/`/api/backends`. Pure refactor,
   `""` viewer = today. Tests: registry filtering.
2. **GitHub OAuth web flow + session cookie**: `/auth/github/*`, signing, middleware on
   preview + admin; skip WS/ACME. Tests: middleware allow/deny, cookie round-trip
   (httptest, GitHub stubbed).
3. **Hub API keys + registration by key**: `POST/DELETE /api/keys`, `X-Hub-Key` on
   `/api/register` → stamp `Owner`; keep `X-Hub-Secret` for open mode. Tests: mint/resolve/
   revoke, register stamps owner, open-mode fallback.
4. **Client**: `mxcli auth hub login/status/logout` (device flow), `run --hub` sends
   `X-Hub-Key` (env → auth.json → legacy secret). Tests: auth.json round-trip, header
   selection.
5. **Wire-up + docs + E2E** against `hub.mxcli.org`: flags in `tunnel-hub`, `run-local`
   skill + docs-site, CLAUDE.md status line; verify owner isolation end-to-end (two
   GitHub users, cross-access = 403).

## Testing

- Unit: registry owner-filtering; cookie sign/verify; middleware allow/deny/redirect;
  key mint/resolve/revoke; header precedence in the client.
- Integration (`-tags integration`, GitHub stubbed via httptest): full web-flow redirect
  chain; register-with-key stamps owner; a second user's cookie gets 403 on the first's
  preview.
- E2E (manual, documented): real GitHub OAuth App against `hub.mxcli.org`, two accounts,
  confirm SSO across subdomains and owner isolation; self-hosted hub with no flags still
  fully open.

## Open questions

1. **GitHub-token-to-hub during key mint** — accept the brief send (simpler) or run the
   full OAuth on the hub so the token never touches mxcli (more friction)? Proposal
   assumes the former; easy to switch.
2. **Key persistence** — in-memory keys are lost on hub restart (users re-run
   `auth hub login`). Add a small on-disk store (`keys.json`, mode `0600`) in slice 3, or
   defer? Registry backends are ephemeral anyway; keys are the only state worth keeping.
3. **Claude Code web bootstrap** — is a repo/environment secret `MXCLI_HUB_KEY` the
   expected UX, or should `mxcli init` help provision it? (Affects the web onboarding
   prompt in `docs-site/src/tools/bootstrap-prompt.md`.)
4. **`mxcli.org` OAuth App ownership** — who registers/owns the OAuth App and holds the
   client secret + session secret for the hosted instance?

## Future work

- **Repo-based / team sharing** (the deferred access model): tie a preview to its GitHub
  repo and grant view to anyone with repo access — needs a **GitHub App** (installations,
  repo-access checks) rather than the OAuth App, and repo identity at registration.
- Per-user chisel tunnel auth (`--authfile` per login) if the shared `TunnelAuth` proves
  insufficient.
- Quotas per owner (max concurrent previews) to bound misuse on the hosted hub.
