# Spike: custom request handler as a re-invokable test entry point

**Status**: **shipped** for `mxcli test --local` — see `cmd/mxcli/testrunner/endpoint.go`.
Measured end-to-end on Mendix **11.13.0**.
**Reproduce the bare spike with**: [`mdl-examples/spikes/test-endpoint-request-handler.mdl`](../../mdl-examples/spikes/test-endpoint-request-handler.mdl)

> The "open issues" at the bottom were what stood between the spike and the
> implementation. The token gate, the loopback check, the test-namespace
> restriction and the javasource cleanup are all now in `endpoint.go`; the
> remaining unresolved items are called out as still open.

## Question

`mxcli test --local` triggers tests from the project's **after-startup microflow**.
That is a boot hook, so re-running a suite requires a full runtime restart by
construction. Can a Java custom request handler give us a *re-invokable* entry
point instead, so a re-run costs an HTTP round trip rather than a restart?

## Answer

Yes, and it is better than the `check_health` idea it replaces — because
`Core.getMicroflowNames()` lets the handler resolve microflows **by name at
request time**. The Java is written once and never regenerated; tests can be
added, edited, and removed with no change to it.

## What was built

A single Java action, `MxTest.RegisterTestEndpoint`, authored inline from MDL
(`CREATE JAVA ACTION … AS $$ … $$`), registering one handler:

```java
Core.addRequestHandler("mxtest/", new RequestHandler() { … });
```

It is called once from a two-line after-startup microflow. After-startup is
still used — but only to *register the endpoint*, never to run tests.

| Route | Behaviour |
|---|---|
| `GET /mxtest/list?prefix=` | Test discovery from `Core.getMicroflowNames()` |
| `GET /mxtest/run?mf=Module.Flow` | `Core.microflowCall(mf).execute(Core.createSystemContext())` |

Results come back as JSON — return value, wall time, and on failure the
**root-cause** exception message.

## Measurements

All on the same machine, same project, Mendix 11.13.0, Postgres local.

| Operation | Time |
|---|---|
| Cold boot to first test invocable (**today's cost per re-run**) | **30.55s** |
| Re-run whole suite, no model change (4 tests, 4 HTTP calls) | **0.084s** |
| Edit a test → watcher rebuild → hot reload → new result over HTTP | **4.29s** |
| Single test invocation, in-process | 0.6–26ms |

So an unchanged re-run goes from ~30s to **0.08s (~360×)**, and an
edit-then-re-run from ~30s to **~4.3s (~7×)** — the 4.3s being almost entirely
the existing `--watch` rebuild, not the test mechanism.

## The load-bearing finding: the handler survives `reload_model`

This is what makes the warm loop actually work, and it was not obvious.

- After-startup does **not** re-run on `reload_model`. Verified by grepping the
  runtime log: exactly two `MxTest request handler registered` lines across two
  *boots*, and none after the reload.
- The runtime **JVM PID is unchanged** across the reload (31312 before and
  after), and `mxcli run --watch` reported `build #2 applied via reload`, not a
  restart.
- The handler object registered by the *old* model resolves the *new* model's
  microflows correctly. Proven with two probes in one reload: an **edited**
  test returned its new value (`2+2=4` → `40+2=42`), and a test **created after
  boot** appeared in `/mxtest/list` and ran — with no restart.

## Consequences for the test runner

Beyond the speed, three structural problems in `cmd/mxcli/testrunner/` dissolve:

1. **The monolithic runner microflow goes away.** `generator.go` currently
   compiles every test into one microflow, regex-renaming variables with `_N`
   suffixes to avoid collisions. With per-name invocation each test is its own
   microflow, so `--filter` and single-test runs are free and one throwing test
   can no longer end the run.
2. **Results stop being scraped from JVM stdout.** They are the HTTP response
   body. `results.go`'s log parsing is no longer on the critical path.
3. **A failing test stops being a failed boot.** Verified: a test throwing
   `MendixRuntimeException` returns HTTP 200 with `ok:false` and the root-cause
   message, and the runtime stays up and serves the next test. Compare
   `runner_local.go:58-71`, which has to special-case boot failure today.

## Issues found by the spike, and how they were resolved

- **The endpoint was unauthenticated.** Verified in the spike: `curl` with no
  cookies and no session executed a microflow. **Resolved** — four gates, each
  verified against a live 11.13.0 runtime:

  | Guard | Verified behaviour |
  |---|---|
  | No `MXCLI_TEST_TOKEN` in the environment | Handler not registered; `/mxtest/list` → 404 |
  | Missing / wrong `X-MxTest-Token` | 401 (constant-time compare) |
  | Non-loopback caller | 403 |
  | `mf` outside `MxTest.Test_*` | 403 |

  The token is generated per run and passed through the runtime's **environment**
  (`LocalRuntimeOptions.Env`), never written into the project — so a failed
  cleanup cannot leave a live credential in `javasource/`.

- **`/list` disclosed the whole app.** Found only by probing the live runtime:
  with no `prefix` the handler returned every microflow in the app,
  `Administration.*` included. **Resolved** — the prefix is clamped to
  `MxTest.Test_`; a caller-supplied prefix can only narrow further. The endpoint
  will not run those microflows, so it must not enumerate them either.

- **Path dispatch was loose** — anything that was not `list` was treated as
  `run`. **Resolved**: exact matches, anything else 404.

- **`DROP JAVA ACTION` leaves the `.java` file behind.** The model document goes;
  the generated source in `javasource/mxtest/` is not the model's to delete.
  **Resolved** — `removeGeneratedJavaSource` removes it, non-fatally.

### Still open

- **There is no `Core.removeRequestHandler`.** The API only offers
  `addRequestHandler`, so the handler cannot be unregistered for the life of the
  JVM. This is why registration is gated rather than reversed. Re-registering
  the same path is still unexercised — after-startup does not re-run on
  `reload_model`, so nothing in the current design hits it.
- **Test parameters and setup/teardown.** Only no-argument microflows are
  invoked. `MicroflowCallBuilder.withParams(Map)` exists, and
  `inTransaction(boolean)` looks directly relevant to rolling back a test's
  database writes — the `@cleanup rollback` annotation is parsed but not yet
  honoured by either mechanism.
- **The Docker path still uses after-startup.** The endpoint needs to hand the
  runtime a secret through its environment and to be reached on loopback,
  neither of which is wired through docker-compose. No Docker daemon was
  available to verify a change there, so it was left alone rather than shipped
  untested.
- ~~`--attach` to an already-running `run --local`~~ — **shipped**, see below.

## The warm loop, realised (`--watch`)

`mxcli test --local --watch` keeps the runtime and the build server up and
re-runs the suite on every change to a test file or to the model. Measured on
the same 11.13.0 app:

| | |
|---|---|
| First run (cold boot) | ~30s |
| Edit a test → verdict on screen | **~2.0s** |
| Edit a microflow under test → verdict on screen | **~2.1s** |
| The tests themselves | 20–70ms |

Every re-run in the session applied via **reload**, not restart — the property
the spike established. Verified live across a session that edited a test,
deleted a test, added a test, and changed the microflow under test.

Two hazards this loop has that the `run --local` dev loop does not:

1. **The runner writes to the project it is watching.** Injecting the test
   microflows moves the very mtime being polled, so the baseline is taken after
   the injection and rebuild settle. Getting it wrong is an infinite rebuild
   loop; verified by idling a session and confirming the run counter does not
   advance.
2. **The injected set changes during the session.** Cleanup drops what is
   *currently* injected, not what was injected at boot — otherwise a test added
   mid-session is left in the user's project. A deleted test's microflow is
   dropped explicitly, since `CREATE OR REPLACE` says nothing about removal and
   a lingering flow would keep reporting a stale pass.

## `--attach`: no boot at all

`mxcli test --attach` runs against an app already up, skipping the boot entirely.
Measured on the same 11.13.0 app: **2.83s** for the first attached run and
**2.30s** for a repeat, against ~30s cold — with the dev app still serving
throughout.

### Why it has to be cooperative

The obvious reading of "attach to a running app" does not work, and the reason is
worth recording because it constrains the design completely:

- The handler is registered by the **after-startup microflow**, which runs only
  at boot. It cannot be added to an app that is already up.
- Its token comes from the **runtime's environment**, which a second process
  cannot change either.

So the app has to opt in *before* it boots: `mxcli run --local --test-endpoint`.
That is also the right place for the decision, since hosting the endpoint means
the developer's own app carries a microflow-executing endpoint and tests will
write to the database they are looking at.

What a second process *can* do, and does, is drive the dev loop's **serve server
and admin API** over loopback — both are plain HTTP. So an attach applies its own
injections deterministically instead of waiting to see whether someone else's
`--watch` noticed; `--attach` does not require the dev loop to be watching.

### The handshake

`run --local --test-endpoint` publishes `<project>/.mxcli/test-endpoint.json`
(mode 0600, written-then-renamed) carrying the app/admin/serve ports, the
endpoint token, the admin password, and its own PID. `--attach` reads it and
refuses a stale one by checking the PID — a dev loop killed with SIGKILL leaves
the file behind, and without the check that surfaces much later as a confusing
connection error.

The project's own after-startup microflow is **chained**, not displaced, so the
dev app still seeds its data and does whatever else it does at boot.

### Two bugs the live run caught that review had not

1. **The admin API and the endpoint use different secrets.** The first attempt
   passed the endpoint token to the M2EE admin API, which failed with
   `Authentication failed` — *after* the test microflows had already been
   injected. The handshake now carries the admin password separately.
2. **`DROP JAVA ACTION` leaves the generated `.java` behind** (found earlier, in
   the same family): the model document is the model's, the source file is not.

### Ownership boundary

An attach adds and removes only its own test microflows. The endpoint, the
after-startup setting and the `MxTest` module belong to the hosting dev loop and
are removed when *it* exits. Verified live: after an attached run the test
microflows were gone, `MxTest.RegisterEndpoint` was still installed, and the app
was still serving HTTP 200.

A change needing a runtime restart (a new entity or association) is refused
rather than half-applied — that runtime belongs to the other process.
