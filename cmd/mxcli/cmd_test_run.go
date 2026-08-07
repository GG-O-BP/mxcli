// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/mendixlabs/mxcli/cmd/mxcli/testrunner"
	"github.com/spf13/cobra"
)

var testRunCmd = &cobra.Command{
	Use:   "test <file|dir> [file|dir...]",
	Short: "Run MDL tests against a Mendix project",
	Long: `Run microflow tests defined in .test.mdl or .test.md files.

Tests use MDL syntax with javadoc-style annotations for expectations:

  /**
   * @test String concatenation
   * @expect $result = 'John Doe'
   */
  $result = CALL MICROFLOW MyModule.ConcatNames(
    FirstName = 'John', LastName = 'Doe'
  );
  /

With --local the app runs on mxcli's own runtime instead of a container — the
same boot as 'mxcli run --local', so no Docker daemon is needed. It uses its own
ports (8081/8091) and its own '<project>_test' database, so a warm 'run --local'
loop can keep serving the same project while tests run.

--local also uses a different, better mechanism to run the tests:

  1. Parses test files and extracts test blocks with @test/@expect annotations
  2. Generates one microflow per test, plus a Java action that registers a
     token-guarded HTTP endpoint
  3. Boots the app once — startup only registers the endpoint, it runs no tests
  4. Invokes each test by name over HTTP; the verdict comes back in the response
  5. Restores original project settings

Because each test is its own microflow invoked on its own, a test that throws
fails only itself instead of ending the run, and results are returned rather
than recovered from the runtime log.

The endpoint is only reachable from loopback, only with a per-run token passed
to the runtime through its environment (never written into your project), and
will only ever invoke the generated MxTest.Test_* microflows. With no token in
the environment it is not registered at all, so a project that kept the MxTest
module through a failed cleanup exposes nothing when deployed elsewhere.

Without --local the Docker path is used instead: the suite is compiled into a
single after-startup microflow, the container is restarted, and results are
parsed out of its log. Pass --legacy-runner to use that mechanism on a local run
too, if the endpoint ever misbehaves.

Supports two file formats:
  .test.mdl  — Pure MDL test blocks separated by /
  .test.md   — Markdown specification with embedded mdl-test code blocks

Examples:
  # Run tests from a test file
  mxcli test tests/microflows.test.mdl -p app.mpr

  # Run all tests in a directory
  mxcli test tests/ -p app.mpr

  # Output JUnit XML for CI
  mxcli test tests/ -p app.mpr --junit results.xml

  # List tests without executing
  mxcli test tests/ -p app.mpr --list

  # Run without Docker, on mxcli's own local runtime
  mxcli test tests/ -p app.mpr --local

  # Skip build (reuse existing deployment)
  mxcli test tests/ -p app.mpr --skip-build

  # Verbose output (show all runtime logs)
  mxcli test tests/ -p app.mpr --verbose
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		projectPath, _ := cmd.Flags().GetString("project")
		list, _ := cmd.Flags().GetBool("list")
		junitOutput, _ := cmd.Flags().GetString("junit")
		skipBuild, _ := cmd.Flags().GetBool("skip-build")
		local, _ := cmd.Flags().GetBool("local")
		legacyRunner, _ := cmd.Flags().GetBool("legacy-runner")
		verbose, _ := cmd.Flags().GetBool("verbose")
		color, _ := cmd.Flags().GetBool("color")
		timeoutStr, _ := cmd.Flags().GetString("timeout")

		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid timeout: %v\n", err)
			os.Exit(1)
		}

		if list {
			// Just list tests, no execution needed
			if err := testrunner.ListTests(args, os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Execution requires a project
		if projectPath == "" {
			fmt.Fprintln(os.Stderr, "Error: --project (-p) is required for test execution")
			os.Exit(1)
		}

		opts := testrunner.RunOptions{
			ProjectPath:  projectPath,
			TestFiles:    args,
			SkipBuild:    skipBuild,
			Local:        local,
			LegacyRunner: legacyRunner,
			Timeout:      timeout,
			JUnitOutput:  junitOutput,
			Verbose:      verbose,
			Color:        color,
			Stdout:       os.Stdout,
			Stderr:       os.Stderr,
		}

		result, err := testrunner.Run(opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if !result.AllPassed() {
			os.Exit(1)
		}
	},
}
