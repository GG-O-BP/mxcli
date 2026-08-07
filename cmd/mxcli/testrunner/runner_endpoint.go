// SPDX-License-Identifier: Apache-2.0

package testrunner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mendixlabs/mxcli/cmd/mxcli/docker"
)

// runViaEndpoint boots the app once and drives the suite over HTTP.
//
// The contrast with the after-startup path is the whole point: there, tests run
// during boot, so every re-run is a restart and a result is something recovered
// from the log. Here boot only registers the endpoint, and each test is a
// request against a runtime that stays up.
func runViaEndpoint(opts RunOptions, suite *TestSuite, token string, timeout time.Duration, w io.Writer) (*SuiteResult, error) {
	logPath := filepath.Join(filepath.Dir(opts.ProjectPath), ".mxcli", "test-runtime.log")

	fmt.Fprintln(w, "Starting local runtime (no Docker)...")
	app, err := docker.StartLocalApp(docker.LocalAppOptions{
		ProjectPath: opts.ProjectPath,
		AppPort:     localTestAppPort,
		AdminPort:   localTestAdminPort,
		ServePort:   localTestServePort,
		DB: docker.DBConfig{
			Name: docker.DeriveDBName(opts.ProjectPath) + localTestDBSuffix,
		},
		EnsureDB:  true,
		SkipBuild: opts.SkipBuild,
		// The token reaches the runtime through its environment and is never
		// written to the project. See endpointTokenEnv.
		Env:            []string{endpointTokenEnv + "=" + token},
		RuntimeLogPath: logPath,
		Stdout:         w,
		Stderr:         w,
	})
	if err != nil {
		// Unlike the after-startup path, a boot failure here is never a test
		// result — no test has run yet. It is always a real error.
		return nil, fmt.Errorf("local runtime: %w", err)
	}
	defer app.Stop()

	client := newEndpointClient(localTestAppPort, token)
	if err := client.waitReady(endpointReadyTimeout(timeout)); err != nil {
		return nil, fmt.Errorf("%w\n  hint: check %s for a registration failure", err, logPath)
	}

	// Ask the app which test microflows it actually has. A test whose microflow
	// is missing is reported as an error against that test rather than failing
	// the run, so one bad test block does not hide every other result.
	present := make(map[string]bool)
	names, err := client.list()
	if err != nil {
		return nil, fmt.Errorf("listing test microflows: %w", err)
	}
	for _, n := range names {
		present[n] = true
	}

	result := &SuiteResult{Name: suite.Name, Started: time.Now()}
	fmt.Fprintf(w, "Running %d test(s) over the test endpoint...\n", len(suite.Tests))

	for _, tc := range suite.Tests {
		flow := testFlowName(tc)
		if !present[flow] {
			result.Tests = append(result.Tests, TestResult{
				ID:      tc.ID,
				Name:    tc.Name,
				Status:  StatusError,
				Message: fmt.Sprintf("microflow %s was not created — the test body may not have compiled", flow),
			})
			continue
		}

		rr, err := client.run(flow)
		if err != nil {
			// A transport failure is not a verdict. Report it against this test
			// and keep going; if the runtime died the rest will say so too.
			result.Tests = append(result.Tests, TestResult{
				ID:      tc.ID,
				Name:    tc.Name,
				Status:  StatusError,
				Message: fmt.Sprintf("calling the test endpoint: %v", err),
			})
			continue
		}

		res := toResult(tc, rr)
		result.Tests = append(result.Tests, res)
		if opts.Verbose {
			fmt.Fprintf(w, "  %s %s (%s)\n", res.Status, res.Name, res.Duration.Round(time.Millisecond))
		}
	}

	result.Duration = time.Since(result.Started)
	return result, nil
}

// endpointReadyTimeout bounds the wait for the endpoint to register. It is
// capped well below the suite timeout: the handler is registered during the
// start action, so if it is not up shortly after the runtime reports started, it
// is not coming.
func endpointReadyTimeout(suiteTimeout time.Duration) time.Duration {
	const cap = 60 * time.Second
	if suiteTimeout > 0 && suiteTimeout < cap {
		return suiteTimeout
	}
	return cap
}

// endpointCleanupCommands returns the MDL that removes what the endpoint path
// injected, in order.
//
// It mirrors cleanupCommands but has more to take out: the registration Java
// action and startup microflow, plus one microflow per test. When Run created
// the MxTest module, dropping the module removes all of it in one statement;
// when the module was already the user's, each generated document is named
// explicitly so nothing of theirs is touched.
func endpointCleanupCommands(st projectState, suite *TestSuite, mxTestPresent bool) []string {
	restore := "ALTER SETTINGS MODEL AfterStartupMicroflow = ''"
	if st.afterStartup != "" {
		restore = "ALTER SETTINGS MODEL AfterStartupMicroflow = " + quoteMDLString(st.afterStartup)
	}
	cmds := []string{restore}
	if !mxTestPresent {
		return cmds
	}
	if st.createdMxTest {
		return append(cmds, "DROP MODULE "+mxTestModule)
	}
	for _, tc := range suite.Tests {
		cmds = append(cmds, "DROP MICROFLOW "+testFlowName(tc))
	}
	return append(cmds,
		"DROP MICROFLOW "+endpointStartupFlow,
		"DROP JAVA ACTION "+endpointRegisterAction,
	)
}

// cleanupEndpoint restores the project after an endpoint run.
//
// As with cleanup, every statement is attempted even after one fails and the
// failures are returned rather than warned about: a half-restored project still
// carries a test endpoint and an after-startup pointing at it.
func cleanupEndpoint(projectPath string, st projectState, suite *TestSuite, w io.Writer) error {
	mxTestPresent := true
	if exists, err := moduleExists(projectPath, mxTestModule); err == nil {
		mxTestPresent = exists
	}
	if mxTestPresent && !st.createdMxTest {
		fmt.Fprintf(w, "  %s module already existed; dropping only the generated documents\n", mxTestModule)
	}
	return runMDLCommands(projectPath, endpointCleanupCommands(st, suite, mxTestPresent))
}

// removeGeneratedJavaSource deletes the .java file the Java action generated.
//
// DROP JAVA ACTION removes the model document; the source file it wrote into
// javasource/ is not the model's to delete, so it is left behind. For a
// generated per-run artifact that is litter in the user's tree — and litter that
// still contains a request handler, which is exactly what should not be left
// lying around. Failure is not fatal: the file is inert without the model
// document, and a cleanup error here would mask the real ones.
func removeGeneratedJavaSource(projectPath string, w io.Writer) {
	dir := filepath.Join(filepath.Dir(projectPath), "javasource", lowerModule(mxTestModule))
	if _, err := os.Stat(dir); err != nil {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(w, "  note: could not remove generated Java source at %s: %v\n", dir, err)
	}
}
