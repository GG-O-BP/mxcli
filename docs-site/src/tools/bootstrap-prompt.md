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

1. **One app, or a solution of several?** One Mendix app is the default. Say
   "solution" if this is several apps in one repo — e.g. a backend that owns the data
   and publishes OData/REST, and a frontend that consumes it. If so, ask for each
   app's name and one line on what it owns, and follow the multi-app deltas below.
2. **App name.** Becomes the `.mpr` file name, the app name in Studio Pro, and the
   path in the session hook, so it is awkward to change later. One PascalCase word,
   letters and digits only — `OrderPortal`, `FieldService`, `ClubAdmin`. Propose one
   from my answer to Q3.
3. **What is the app for?** One or two sentences: who uses it, and what it lets them
   do. If my answer is vague ("a tool for work"), ask one follow-up — everything below
   is derived from this.
4. **What does it keep track of?** Three to six nouns that will become entities, and a
   word on how they relate (e.g. "a Job has many Visits; each Visit has Photos"). For
   a solution, also ask which app owns each noun.
5. **Who logs in?** The user roles, and roughly what each may do (e.g. "Requester
   creates and sees their own; Approver sees everything and approves").
6. **Look and feel.** One of the bundled themes: `signal` (light, high contrast),
   `ledger` (light, dense, data-heavy), `console` (dark), or `none` for stock Atlas.
   Default `signal`.
7. **Mendix version.** Default `11.13.0`.

If I say "defaults" or ignore a question, choose something sensible for it, tell me
what you chose in one line, and keep going — do not block on me twice.

## Then provision

Substitute my answers for `<AppName>`, `<version>` and `<theme>` throughout. For a
solution, do steps 2–4 once per app and read "If this is a solution" first.

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
5. Write the brief to `README.md` at the repo root: the app name(s), my answers to
   Q3–Q5 in my words, and the theme and Mendix version you used. For a solution, say
   which app owns what and how they talk to each other. This is what tells the next
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

## If this is a solution (several apps in one repo)

Each app is a full Mendix project — one `.mpr`, one runtime, one database. Same steps,
with these deltas:

- **Layout.** One subfolder per app, nothing at the repo root but `README.md`,
  `FINDINGS.md` and `.claude/`. Run `mxcli new <AppName> --version <version> --theme
  <theme>` once per app and leave each where it lands; do not move anything up.
- **Ports.** Every app defaults to 8080/8090/6543 and they will collide. Give the
  first app the defaults and the second `--app-port 8180 --admin-port 8190
  --serve-port 6643`. Avoid 8081/8091/6544 — `mxcli test --local` uses those.
- **Give each app its own hostname**, not just its own port. Cookies are keyed on
  host name and **ignore the port**, so two apps on `localhost:8080` and
  `localhost:8180` share one cookie jar: logging into one can silently replace the
  other's `XASSESSIONID`. Two hostnames give two jars, and the differing ports do no
  harm. Add them to `/etc/hosts` —

  ```
  127.0.0.1  backend.local frontend.local
  ```

  — and browse `http://backend.local:8080/` and `http://frontend.local:8180/`. The
  runtime binds `127.0.0.1` and serves any `Host` you send it, and the client uses
  relative URLs, so it works under any name that resolves to loopback. (`*.nip.io`
  works too if you would rather not touch `/etc/hosts`; prefer `/etc/hosts` in a
  locked-down container, where public wildcard DNS may not resolve — `localtest.me`
  resolves to `::1` in some of them.)

  Then record the name in each app's own configuration, so the runtime knows the URL
  it is reached at and generates absolute URLs — OIDC/SAML redirect URIs, deep links
  — against the host name rather than the listen address:

  ```sql
  alter settings configuration 'Default'
    ApplicationRootUrl = 'http://backend.local:8080/';
  ```

  `run --local` picks that up at boot and prints which configuration it came from.
  A blank app ships `http://localhost:8080/` there, and that stock loopback value is
  deliberately ignored — otherwise every project would start advertising a URL, and
  the wrong port under `--app-port`. Only a real host name is passed through.
- **Databases** need no action: the name is derived from the `.mpr` file name, so
  differently-named apps get different databases.
- **The session hook.** `mxcli init` writes `.claude/settings.json` inside each app
  folder, but Claude Code reads the one at the **repo root** — and it will not add a
  second entry for you (it dedupes on the command, not on the project). Write the root
  one yourself, one line per app, e.g.
  `test -x backend/mxcli && (cd backend && ./mxcli run --local --setup --ensure-db -p Backend.mpr) || true`.
  Verify it by checking that a fresh shell can boot each app.
- **Previews.** Pass `--hub-solution <SolutionName>` to every `run --hub` so the apps
  appear grouped in the hub overview instead of as unrelated previews.

**Wire the integration in dependency order — the producer must be running first.**
`CREATE ODATA CLIENT` fetches the `$metadata` at the moment you create it and caches
it in the model; if the URL is unreachable it warns and leaves the client unvalidated,
with no external entities to import. So: publish on the producer
(`CREATE ODATA SERVICE … publish entity …`), boot it (`run --local`), and only then,
on the consumer, `CREATE ODATA CLIENT … MetadataUrl: 'http://backend.local:8080/odata/…/$metadata'`
followed by `CREATE EXTERNAL ENTITIES FROM …`. Use the hostname here too, so the
cached contract and the constant below agree with what the browser sees. Point `ServiceUrl` at a **constant**
(`ServiceUrl: @Module.SvcUrl`) so the address can be changed per environment without
touching the model — it will not stay `localhost`. `mxcli syntax odata.publish` and
`mxcli syntax odata.consume` have the full syntax; business events
(`mxcli syntax business-events`) are the alternative when the link should be
asynchronous.

## Then propose the model — do not build it yet

The blank template ships a `MyFirstModule`; the app's own work belongs in a module
named after it. From the brief, propose in chat:

- a module name, and the entities from Q4 with their attributes and associations
- the user roles from Q5 and what each may read/write
- the handful of pages that make it usable
- for a solution: which app owns each entity, and what crosses the boundary — publish
  only what the other app actually needs

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

## Which Mendix version to ask for

The prompt defaults to the newest version that has a published MxBuild — everything
mxcli does starts with downloading it, so "supported" means "on the CDN". Check before
bumping the default:

```bash
curl -sI -o /dev/null -w '%{http_code}\n' https://cdn.mendix.com/runtime/mxbuild-11.13.0.tar.gz   # 200
curl -sI -o /dev/null -w '%{http_code}\n' https://cdn.mendix.com/runtime/mendix-11.13.0.tar.gz    # 200 (runtime)
```

Both have to answer `200` — `run --local` needs the runtime tarball as well as
MxBuild. In a solution, give every app the **same** version: they share the
`~/.mxcli/mxbuild` cache, and a mismatch means a second multi-hundred-MB download and
two runtimes to keep straight.

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

In a solution, run one loop per app from its own folder, with the second app on the
alternate ports, and start the producer first so the consumer's external entities
resolve:

```bash
(cd backend  && ./mxcli run --local -p Backend.mpr --watch)
(cd frontend && ./mxcli run --local -p Frontend.mpr --watch \
                  --app-port 8180 --admin-port 8190 --serve-port 6643)
```

With `127.0.0.1 backend.local frontend.local` in `/etc/hosts`, browse them at
`http://backend.local:8080/` and `http://frontend.local:8180/` so each app gets its
own cookie jar.

See [mxcli run --local](run-local.md) for the warm loop, `--watch`, `--ensure-db`, and
the screenshot flags.
