# Debug Microflows — `mxcli debug`

`mxcli debug` drives the Mendix runtime's **microflow debugger** from the command
line: set breakpoints **by name**, inspect a paused microflow's variables, and
step/continue — against an app started by [`mxcli run --local`](run-local.md). It
is the headless counterpart to Studio Pro's debugger.

Because mxcli owns both halves — the admin password + app URL (from `run --local`)
and the activity model GUIDs (from the `.mpr`) — it can offer breakpoints **by
name**, so you never handle raw GUIDs.

## Quick start

```bash
# terminal 1 — app with the debugger enabled and a session ready
mxcli run --local -p app.mpr --debug

# terminal 2 — break by name, then inspect/step/continue (same -p)
mxcli debug activities Sudoku.ACT_Hint -p app.mpr
mxcli debug break Sudoku.ACT_Hint --activity 'Retrieve' -p app.mpr
#   ...trigger the microflow in the browser (the request pauses)...
mxcli debug paused   -p app.mpr
mxcli debug inspect Game -p app.mpr
mxcli debug step over    -p app.mpr
mxcli debug continue     -p app.mpr
mxcli debug disable      -p app.mpr        # always finish here
```

`--debug` on `run --local` enables the debugger and starts a session (cached under
`<projectDir>/.mxcli/`), so you skip a separate `mxcli debug enable`. With **no
breakpoints set, nothing pauses** — `--debug` alone is behaviour-neutral.

## Two APIs, one command

Under the hood the debugger spans two runtime APIs, which `mxcli debug` hides:

- the **M2EE admin** API toggles the debugger (`enable`/`disable`/`status`);
- the app's **`/debugger/`** endpoint runs the session (breakpoints, paused state,
  stepping).

## Commands

| Command | What it does |
|---------|--------------|
| `mxcli debug status` | Debugger on? How many microflows paused? |
| `mxcli debug enable` / `disable` | Turn on/off (prefer `run --local --debug` for the warm loop) |
| `mxcli debug activities <Module.Flow>` | List activities + the object IDs you can break on |
| `mxcli debug break <Module.Flow> --activity <#n\|caption> [--if <expr>]` | Set a breakpoint by name; `--if` is a conditional (Mendix expression) |
| `mxcli debug unbreak <Module.Flow> --activity <#n\|caption>` | Clear a breakpoint |
| `mxcli debug breaks` | List the breakpoints set this session (name → object ID) |
| `mxcli debug paused` | Paused microflows + full state (variables) |
| `mxcli debug inspect <var> [--flow <debug_id>]` | Inspect one variable of a paused flow |
| `mxcli debug step [over\|into\|out] [--flow <debug_id>]` | Advance one step (default `over`) |
| `mxcli debug continue [--all]` | Resume the paused flow (or all with `--all`) |

**Selecting an activity:** `--activity '#2'` (the index from `activities`) or a
caption substring like `--activity 'Retrieve'` (case-insensitive; must match one).

**Selecting a paused flow:** `--flow <debug_id>` (from `paused`); a single paused
flow is auto-selected.

## Connection flags

Defaults target a `run --local` runtime; override to debug a differently-configured
or remote runtime.

| Flag | Env | Default |
|------|-----|---------|
| `--app-url` | `MXCLI_APP_URL` | `http://127.0.0.1:8080` |
| `--admin-port` | — | `8090` |
| `--admin-pass` | `MXCLI_ADMIN_PASS` | `mxcli-local-dev` |
| `--debug-pass` | `MXCLI_DEBUG_PASS` | `mxdebug` |
| `-p, --project` | — | (for the `.mxcli/` session + breakpoint files) |

## Important behaviour

- **A breakpoint pauses whoever hits it — the browser included.** The triggering
  request hangs until `continue` or `disable`. Don't leave a paused session idle.
- **Always `mxcli debug disable` when done.** `run --local --debug` does this on
  shutdown; a manual `enable` is your responsibility.
- **Use the same `-p` for every command.** The session token and breakpoint record
  live under `<projectDir>/.mxcli/`.
