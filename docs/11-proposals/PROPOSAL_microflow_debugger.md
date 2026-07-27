---
title: mxcli microflow debugger — breakpoints by name against a running runtime
status: proposed
date: 2026-07-26
---

# Proposal: `mxcli debug` — the Mendix microflow debugger, breakpoints by name

**Status:** Proposed
**Date:** 2026-07-26
**Author:** Generated with Claude Code

Add first-class microflow-debugger support to mxcli: set breakpoints, inspect paused
microflows and their variables, and step/continue — all **by microflow (and activity)
name**, against a runtime started by `mxcli run --local` (or any reachable runtime).

Origin: the `ako/mxcli-sudoku` findings + README documented the full debugger protocol
and a hand-rolled `scripts/mfdebug.sh` wrapper, and concluded that **mxcli is the only
tool positioned to offer breakpoints by name** — it already owns both halves: the admin
password + app URL (from `run --local`) and the activity model GUIDs (from the `.mpr`).
This proposal turns that wrapper into a supported command.

## Problem

Debugging a server-side microflow today means either opening Studio Pro (defeats the
headless `run --local` loop) or driving the runtime's debugger APIs by hand. The latter
is awkward because:

- It spans **two** APIs with **two** auth schemes (see *Background*).
- A breakpoint's `object_id` is the **model GUID of an activity**, stored in the `.mpr`
  as a little-endian .NET GUID — "nothing in the runtime will tell you what it is." The
  sudoku wrapper shells out to `mxcli bson dump` + a Python GUID conversion to find it.
- `params` is mandatory on every debugger call even when empty; the session token must be
  threaded through; and a live breakpoint **pauses whoever hits it (the browser too)**,
  so a forgotten `disable` hangs the app.

mxcli already parses microflows (`sdk/mpr/parser_microflow.go` → each activity carries a
`$ID` via `extractBsonID`) and already owns the admin connection (`docker.CallM2EE`), so it
can hide all of this behind names.

## Background: the runtime debugger protocol (as verified by sudoku)

**Plane 1 — M2EE admin (`:8090`, `X-M2EE-Authentication: base64(adminPass)`)** toggles
debugger state only:

| Action | Params | Purpose |
|---|---|---|
| `enable_debugger` | `{"password": "<debugPass>"}` | turn on; **password is required** |
| `disable_debugger` | — | turn off |
| `get_debugger_status` | — | `{enabled, client_connected, number_of_paused_microflows}` |

**Plane 2 — app debugger endpoint (`<app>/debugger/`,
`X-Debugger-Authentication: base64(debugPass)`)** drives breakpoints. Body is always
`{action, session_token, params}` and **`params` is mandatory even when `{}`**:

| Action | Params | Notes |
|---|---|---|
| `start_session` | `{breakpoints: []}` | **no token**; response has `result.session_token` |
| `add_breakpoint` | `{microflow_name, object_id, condition?}` | `object_id` = activity model GUID |
| `remove_breakpoint` | `{object_id}` | |
| `get_paused_microflows` | `{}` | flows + all in-scope variables |
| `get_object` | `{debug_id, variable_name}` | inspect one variable |
| `step_over` / `step_into` / `step_out` | `{debug_id}` | |
| `continue` / `continue_all` | `{}` | resume |

Auth quirk (documented): the debugger endpoint accepts only
`X-Debugger-Authentication: base64(pass)` — raw password, `Basic`, and `Bearer` all 401,
and the 401 body is `{}` with no `WWW-Authenticate`.

## Goals

- Set/clear breakpoints, list paused microflows with variables, inspect a variable, step,
  and continue — **by microflow name** (and activity name/index), never a raw GUID.
- Reuse `run --local`'s admin password and app URL automatically (zero flags in the
  common case).
- Resolve activity GUIDs from mxcli's own model — no `bson dump`/Python detour.
- Fail safe: make it hard to leave the debugger enabled (auto-disable, clear warnings).

## Non-goals (this cut)

- A DAP (Debug Adapter Protocol) bridge for VS Code / the vscode-mdl extension (attractive
  follow-up — see *Future work*).
- Nanoflow (client-side) debugging — this is the **microflow** (server) debugger.
- Conditional-breakpoint expression validation beyond passing `condition` through.
- Replacing Studio Pro's debugger UI; this is the headless/CLI counterpart.

## Design

### Command surface

```
mxcli debug status                 # get_debugger_status (enabled? paused? client connected?)
mxcli debug enable [--debug-pass]  # enable_debugger + start_session, cache session token
mxcli debug break  Module.Flow [--activity <name|#index>] [--if '<expr>']
mxcli debug breaks                 # list active breakpoints (name → activity → GUID)
mxcli debug unbreak Module.Flow [--activity …]      # remove_breakpoint
mxcli debug paused                 # get_paused_microflows: flow, current activity, vars
mxcli debug inspect <var> [--flow …]                # get_object for a paused flow
mxcli debug step [over|into|out]   # default: over
mxcli debug continue [--all]
mxcli debug disable                # disable_debugger (also run on Ctrl-C / defer)
```

- **Name → GUID resolution.** `break Module.Flow --activity ACT_Name` looks up the
  microflow in the model, finds the activity by caption/name (or `#index` in flow order),
  and uses its `$ID` as `object_id`. Bare `break Module.Flow` (no `--activity`) breaks on
  the flow's **start** activity. `mxcli debug breaks` prints the reverse map so a user sees
  what a GUID corresponds to.
- **Session/token handling** is internal: `enable` (or the first `break`) does
  `start_session`, caches the token under the project (e.g. `.mxcli/debug-session.token`),
  and every subsequent call threads it. `disable` clears it.
- **Human + JSON output.** `paused`/`inspect` render a readable tree by default and
  `--format json` for scripting/agents.

### Reusing `run --local`

`run --local` already binds the admin API at `mxcli-local-dev` on `:8090` and serves the
app on `:8080`. `mxcli debug` resolves the same defaults (admin pass, app URL, project) so
the common case is flagless. Overridable with `--admin-url/--app-url/--admin-pass/--debug-pass/-p`
and env vars mirroring the sudoku wrapper (`MXCLI_ADMIN_PASS`, `MXCLI_DEBUG_PASS`, …).

Optional convenience: `mxcli run --local --debug[=pass]` enables the debugger at boot so a
fresh session is immediately debuggable.

### Code shape

- **Admin plane**: reuse `docker.CallM2EE` for `enable_debugger`/`disable_debugger`/
  `get_debugger_status` (it already speaks `{action, params}` + `X-M2EE-Authentication`).
- **Debugger plane**: a small new client `docker.DebuggerClient` (or `mdl/.../debug`) —
  POST `<app>/debugger/` with `X-Debugger-Authentication`, the `{action, session_token,
  params}` envelope, and typed responses. `params` always serialized (never omitted).
- **GUID resolver**: a helper over the existing microflow reader that maps
  `Module.Flow[.activity]` → `object_id` and back. `extractBsonID` already yields the
  activity `$ID`; confirm its string form matches the debugger's `object_id`
  (little-endian .NET GUID → canonical UUID) and normalize if needed.
- **CLI**: `cmd/mxcli/cmd_debug.go` (Cobra), subcommands as above.

## Safety

- **Enabling the debugger changes runtime behaviour**: a breakpoint pauses *any* execution
  that reaches it — including a browser request, which then hangs until `continue`. Every
  command that can leave it enabled prints this once, and `mxcli debug` registers a
  best-effort `disable` on interrupt.
- `status` surfaces `number_of_paused_microflows` so a hung app is diagnosable at a glance.
- The debugger password is a local dev secret (default `mxdebug`, overridable); document
  not enabling the debugger on a shared/hosted runtime.

## Implementation slices

1. **Debugger client + admin toggles**: `DebuggerClient`, `enable/disable/status`,
   `start_session` + token cache. Tests with an httptest stub of both planes.
2. **GUID resolver + `break`/`unbreak`/`breaks`**: name→activity→`object_id`; reverse map.
   Tests over a fixture microflow (assert the resolved GUID equals the model `$ID`).
3. **Inspect/step/continue + `paused`**: response rendering (tree + `--format json`).
4. **`run --local` integration + safety**: default resolution from the warm loop,
   `--debug` boot flag, interrupt-disable, warnings.
5. **Docs + skill**: a `debug-microflows` skill, `mxcli debug` help, docs-site page, and a
   worked example mirroring the sudoku session (enable → break by name → paused → step →
   continue → disable).

## Testing

- Unit: envelope/auth (both planes) against httptest stubs; token threading; `params`
  always present; GUID resolver name↔GUID over a fixture.
- Integration (`-tags integration`): against a `run --local` runtime — enable, break on a
  known activity by name, drive a request that hits it, assert `get_paused_microflows`
  shows the flow + variables, step, continue, disable; assert `status` returns to
  `enabled:false`.

## Open questions

1. **Activity naming** — break by activity **caption** (user-facing, may be blank/dup) vs
   a stable **`#index`** in flow order vs both? Proposal offers both, caption first.
2. **GUID form** — verify `extractBsonID`'s output equals the debugger's expected
   `object_id` string across versions; if the runtime wants a different casing/format,
   normalize in the resolver.
3. **Interactive REPL** — ship one-shot subcommands first (scriptable, agent-friendly), or
   also an interactive `mxcli debug repl` stepping loop? Proposal: subcommands first.
4. **Default-on with `run --local`?** — leave the debugger opt-in (`--debug`), or always
   enable it locally? Proposal: opt-in (it changes runtime behaviour).

## Future work

- **DAP bridge** so the vscode-mdl extension (and any DAP client) can set breakpoints in
  `.mdl`/microflow views and step with a real debugger UI — mxcli becomes the debug
  adapter over this same protocol.
- Nanoflow (client-side) debugging.
- `mxcli debug watch <expr>` and conditional-breakpoint expression checking via the
  existing `exprcheck` package.
