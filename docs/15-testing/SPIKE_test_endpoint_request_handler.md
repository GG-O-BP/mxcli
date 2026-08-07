# Spike: custom request handler as a re-invokable test entry point

**Status**: spike complete, measured end-to-end on Mendix **11.13.0**.
**Reproduce with**: [`mdl-examples/spikes/test-endpoint-request-handler.mdl`](../../mdl-examples/spikes/test-endpoint-request-handler.mdl)

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

## Open issues before this becomes a feature

- **The endpoint is unauthenticated.** Verified: `curl` with no cookies and no
  session executed a microflow. A handler that runs arbitrary microflows by
  name under a **system context** is a remote code execution primitive. It must
  be gated on a per-run random token (header or query param) that mxcli
  generates and passes, and it should refuse to register unless the runtime is
  a local dev/test instance. Non-negotiable before this ships.
- **There is no `Core.removeRequestHandler`.** The API only offers
  `addRequestHandler`. The handler cannot be unregistered for the life of the
  JVM — a further reason to gate registration rather than rely on removal.
  Re-registering the same path was never exercised (after-startup does not
  re-run), so its behaviour is unknown.
- **Path dispatch is loose.** Anything that is not `list` is treated as `run`.
  Real routing needed.
- **Test parameters and setup/teardown are unexplored.** The spike only ran
  no-argument microflows. `MicroflowCallBuilder.withParams(Map)` exists, and
  `inTransaction(boolean)` looks directly relevant to rolling back a test's
  database writes — neither was exercised.
- **Which project owns the Java action.** The spike wrote `MxTest` into the
  project under test. Whether the runner injects it per-run, or it is shipped
  as a small module, is a design decision this spike does not settle.
