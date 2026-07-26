// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/mendixlabs/mxcli/cmd/mxcli/docker"
	"github.com/spf13/cobra"
)

// cmd_debug.go is slice 1 of the microflow debugger (see
// docs/11-proposals/PROPOSAL_microflow_debugger.md): connect to a runtime's
// debugger and toggle it (status/enable/disable), starting a session on enable.
// Breakpoints, paused-microflow inspection, and stepping are later slices.

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug microflows against a running Mendix runtime",
	Long: `Drive the Mendix runtime microflow debugger.

The debugger spans two APIs: the M2EE admin plane toggles it on/off, and the
app's /debugger/ endpoint runs the debug session. Defaults match a runtime
started by 'mxcli run --local' (app http://127.0.0.1:8080, admin :8090, admin
password "mxcli-local-dev"), so in the common case no flags are needed.

Enabling the debugger CHANGES runtime behaviour: once breakpoints exist (a later
slice), any execution that reaches one pauses until you continue — including a
browser request, which will hang. Always finish with 'mxcli debug disable'.

Examples:
  mxcli debug status
  mxcli debug enable
  mxcli debug disable`,
}

var debugStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the debugger is enabled and how many microflows are paused",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		st, err := c.Status()
		if err != nil {
			return err
		}
		fmt.Printf("Debugger: %s\n", enabledLabel(st.Enabled))
		fmt.Printf("Debug client connected: %v\n", st.ClientConnected)
		fmt.Printf("Paused microflows: %d\n", st.NumberOfPausedMicroflows)
		if st.NumberOfPausedMicroflows > 0 {
			fmt.Println("\nNote: paused microflows hold their requests open until 'mxcli debug continue' (a later slice) or 'mxcli debug disable'.")
		}
		return nil
	},
}

var debugEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable the debugger and start a debug session",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Enable(); err != nil {
			return err
		}
		if _, err := c.StartSession(); err != nil {
			return fmt.Errorf("debugger enabled but starting a session failed: %w", err)
		}
		fmt.Println("Debugger enabled; debug session started.")
		fmt.Println("Breakpoints/stepping arrive in a later slice; for now the session is ready.")
		fmt.Println("Remember to run 'mxcli debug disable' when done — a breakpoint pauses whoever hits it, the browser included.")
		return nil
	},
}

var debugDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable the debugger and clear the cached session",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		if err := c.Disable(); err != nil {
			return err
		}
		fmt.Println("Debugger disabled.")
		return nil
	},
}

// resolveDebuggerClient builds a DebuggerClient from the flags/env, defaulting to
// a `run --local` runtime. The admin host is derived from --app-url so a remote
// runtime works too; the token is cached under <projectDir>/.mxcli/.
func resolveDebuggerClient(cmd *cobra.Command) (*docker.DebuggerClient, error) {
	appURL, _ := cmd.Flags().GetString("app-url")
	adminPort, _ := cmd.Flags().GetInt("admin-port")
	adminPass, _ := cmd.Flags().GetString("admin-pass")
	debugPass, _ := cmd.Flags().GetString("debug-pass")
	project, _ := cmd.Flags().GetString("project")

	u, err := url.Parse(appURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid --app-url %q", appURL)
	}

	dir := "."
	if project != "" {
		dir = filepath.Dir(project)
	}
	tokenPath := filepath.Join(dir, ".mxcli", "debug-session.token")

	c := docker.NewDebuggerClient(docker.DebuggerOptions{
		Admin: docker.M2EEOptions{
			Host:   u.Hostname(),
			Port:   adminPort,
			Token:  adminPass,
			Direct: true,
		},
		AppURL:    appURL,
		DebugPass: debugPass,
		TokenPath: tokenPath,
	})
	// Best-effort: pick up a session token cached by a prior 'enable'.
	_ = c.LoadToken()
	return c, nil
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func init() {
	debugCmd.PersistentFlags().StringP("project", "p", "", "Path to the .mpr (used for the session-token cache location)")
	debugCmd.PersistentFlags().String("app-url", envOr("MXCLI_APP_URL", "http://127.0.0.1:8080"), "App base URL hosting the /debugger/ endpoint")
	debugCmd.PersistentFlags().Int("admin-port", 8090, "M2EE admin API port")
	debugCmd.PersistentFlags().String("admin-pass", envOr("MXCLI_ADMIN_PASS", "mxcli-local-dev"), "M2EE admin password")
	debugCmd.PersistentFlags().String("debug-pass", envOr("MXCLI_DEBUG_PASS", "mxdebug"), "Debugger password (passed to enable_debugger and used as the debugger-endpoint credential)")

	debugCmd.AddCommand(debugStatusCmd)
	debugCmd.AddCommand(debugEnableCmd)
	debugCmd.AddCommand(debugDisableCmd)
	rootCmd.AddCommand(debugCmd)
}
