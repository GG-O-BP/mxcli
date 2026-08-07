# Bootstrap prompt (empty repo → running Mendix app)

The **primary** way to start a Mendix + mxcli project from the web or an iPad — no
local CLI, no GitHub template to pick from a (short) mobile list. Open an **empty
repo** in Claude Code Web and paste the prompt below; the agent asks you what the app
is, then provisions everything and commits the result so future sessions
self-bootstrap.

The interview comes first for a reason: the app name becomes the `.mpr` file name, the
Studio Pro app name and the path baked into the SessionStart hook, so it is far cheaper
to ask than to rename afterwards. The rest of the answers are the brief — they get
written into the repo, so the session that resumes after an idle reap knows what it is
building.

Why a prompt instead of a GitHub template repo: the mobile "New repository" template
dropdown shows only a small subset of templates, and a template repo needs per-Mendix-
version upkeep. A prompt starts from a *truly empty* repo, runs *current* mxcli, and
can seed the model from a design prototype in the same session — nothing to maintain.

## The prompt

````text
This is an empty repo. You are going to provision it as a Mendix app developed with
mxcli — but first find out what the app is.

## Step 0 — interview me, and WAIT for my answers before running anything

Ask all of these in ONE message, numbered, each with the default you would pick, so I
can reply "defaults" or answer only the ones I care about. Do not start provisioning
until I have replied.

1. **App name.** Becomes the `.mpr` file name, the app name in Studio Pro, and the
   path in the session hook, so it is awkward to change later. One PascalCase word,
   letters and digits only — `OrderPortal`, `FieldService`, `ClubAdmin`. Propose one
   from my answer to Q2.
2. **What is the app for?** One or two sentences: who uses it, and what it lets them
   do. If my answer is vague ("a tool for work"), ask one follow-up — everything below
   is derived from this.
3. **What does it keep track of?** Three to six nouns that will become entities, and a
   word on how they relate (e.g. "a Job has many Visits; each Visit has Photos").
4. **Who logs in?** The user roles, and roughly what each may do (e.g. "Requester
   creates and sees their own; Approver sees everything and approves").
5. **Look and feel.** One of the bundled themes: `signal` (light, high contrast),
   `ledger` (light, dense, data-heavy), `console` (dark), or `none` for stock Atlas.
   Default `signal`.
6. **Mendix version.** Default `11.6.3`.

If I say "defaults" or ignore a question, choose something sensible for it, tell me
what you chose in one line, and keep going — do not block on me twice.

## Then provision

Substitute my answers for `<AppName>`, `<version>` and `<theme>` throughout.

1. Ensure `mxcli` is available. It should be pre-installed by the environment; if
   not, download a prebuilt binary for your OS/arch and put it at `./mxcli`, e.g.:

   ```bash
   curl -fsSL -o ./mxcli \
     https://github.com/mendixlabs/mxcli/releases/download/nightly/mxcli-linux-amd64
   chmod +x ./mxcli
   ```

   Use the **`nightly`** build: this is fast-moving alpha software and the warm-loop
   commands used below (`run --local`, `--setup`, `--ensure-db`) land in `nightly`
   before they appear in a tagged release. For a reproducible setup, pin a specific
   release instead (`.../releases/download/vX.Y.Z/mxcli-<os>-<arch>`). Note: `go
   install …@latest` does **not** work — the generated ANTLR parser isn't committed,
   so use the prebuilt binary (a from-source build needs `make grammar`).
2. Create the app, and put it at the **repo root** — that is where `.claude/` and the
   `./mxcli` binary have to live for future sessions to self-bootstrap. `mxcli new`
   refuses to write into a directory that is not empty, and a git repo always has
   `.git`, so create it in a subfolder and move it up:

   ```bash
   ./mxcli new <AppName> --version <version> --theme <theme>
   shopt -s dotglob && mv <AppName>/* . && rmdir <AppName>
   ```

   (Use `mxcli init` instead if an `.mpr` already exists.) `mxcli new` also runs
   `mxcli init`, which writes `.claude/settings.json` with a SessionStart hook
   pointing at `-p <AppName>.mpr` — check that the path in it is right after the move.
3. Confirm the Claude tooling: `./mxcli init --tool claude` (idempotent — it is what
   step 2 already ran, and re-running it is the cheapest way to be sure the hook,
   skills and commands are in place).
4. Bring prerequisites up: `./mxcli run --local --setup --ensure-db -p <AppName>.mpr`
   (caches MxBuild + runtime, starts Postgres, creates the app database).
5. Write the brief to `README.md` at the repo root: the app name, my answers to Q2–Q4
   in my words, and the theme and Mendix version you used. This is what tells the next
   session — after an idle reap, with none of this conversation — what it is building.
   Keep it short enough that it stays true.
6. Create a `FINDINGS.md` at the repo root and keep appending to it as you work.
   Log anything surprising or broken: an mxcli command that errored, a workaround you
   applied, a `mxcli check` that passed but a real `mx check` later flagged. Note the
   Mendix + mxcli versions and how each finding was verified. This is durable context
   for the next session, and the most useful thing to share back to improve mxcli.
7. COMMIT everything now — `<AppName>.mpr`, `.devcontainer/`, `.claude/` (including the
   SessionStart hook), `README.md` and `FINDINGS.md` — so that after idle reaping the
   next session bootstraps from files, not from re-running this prompt.
8. Boot and verify: `./mxcli run --local -p <AppName>.mpr` in the background, then
   confirm the app answers HTTP 200 at http://localhost:8080/ and report.
9. (Optional) For a browser preview from this cloud session, run
   `./mxcli run --hub https://hub.mxcli.org -p <AppName>.mpr` and report the preview
   URL it prints. This needs `MXCLI_HUB_KEY` set on the environment (see the workflow
   page); without it, continue as a normal local run.

## Then propose the model — do not build it yet

The blank template ships a `MyFirstModule`; the app's own work belongs in a module
named after it. From the brief, propose in chat:

- a module name, and the entities from Q3 with their attributes and associations
- the user roles from Q4 and what each may read/write
- the handful of pages that make it usable

Show me that as MDL I can read, and wait for my go-ahead before executing it. If I
gave you a design to work from, use it as the source of truth for the model and the
pages: <paste or link a design here — otherwise ignore this line>.
````

## Which mxcli version gets installed

Prebuilt binaries are the working install path. CI publishes them on every `vX.Y.Z`
tag (latest is v0.16.0) **and** as a rolling `nightly` pre-release, with assets named
`mxcli-<os>-<arch>`.

- **`nightly` — recommended while mxcli is fast-moving alpha.** New features (the whole
  warm-loop surface: `run --local`, `--watch`, `--ensure-db`, `--setup`, screenshots)
  land in `nightly` before they reach a tagged release, so the bootstrap flow above
  needs it. Download `.../releases/download/nightly/mxcli-<os>-<arch>`, or once mxcli is
  present, `mxcli setup mxcli --tag nightly`.
- **`vX.Y.Z` — pin for reproducibility / stability.** The CI marks nightly a
  pre-release ("use tagged releases for production"). Download
  `.../releases/download/vX.Y.Z/mxcli-<os>-<arch>` or `mxcli setup mxcli --tag vX.Y.Z`.
  With no `--tag`, `mxcli setup mxcli` matches the mxcli already running it (nightly →
  `nightly`, `vX.Y.Z` → that release) — mainly useful for replicating a version onto
  another OS/arch (e.g. the Linux binary in a Dev Container), not the first install.
- **Environment pre-install** (the robust path) installs whatever the Claude Code Web
  image bakes in — the way to pin a known-good version fleet-wide.
- **`go install …@latest` does not work yet.** The module *is* public (tags v0.1.0–
  v0.16.0), but the generated ANTLR parser (`mdl/grammar/parser/`) is gitignored and
  not committed, so a `go install` from the tagged source fails on the missing package.
  Building from source works only via `make build`/`make release` (which run
  `make grammar` first). Enabling `go install` would require committing the generated
  parser (or generating it during module build) — a maintainer decision.

## Two rules that make this robust

- **Committing the config (step 7) is mandatory.** The prompt is a *one-time seed*.
  Its output — `.mpr` + `.devcontainer/` + `.claude/` with the SessionStart hook — must
  be committed so the steady state is file-driven and deterministic. After that, every
  new session runs the hook (`run --local --setup --ensure-db`) automatically; you
  never re-paste the prompt.
- **mxcli delivery is an environment concern, not the prompt's.** Step 1 is the fragile
  part in a gated web session (a GitHub release `curl` may be blocked). The robust fix
  is for the Claude Code Web **environment image / setup script to pre-install mxcli**
  (and pre-cache MxBuild + runtime); `go install` via `proxy.golang.org` is the fallback
  and needs mxcli published as a public Go module.

## After bootstrap — the inner loop

```bash
./mxcli run --local -p <AppName>.mpr --watch --screenshot   # warm dev loop + screenshots
./mxcli exec change.mdl -p <AppName>.mpr                     # edit the model; the loop hot-applies
```

See [mxcli run --local](run-local.md) for the warm loop, `--watch`, `--ensure-db`, and
the screenshot flags.
