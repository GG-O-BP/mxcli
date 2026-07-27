// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
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
		// Clear the local breakpoint record too — the runtime dropped them with the
		// session, so mxcli's view must not claim they're still set.
		_ = os.Remove(breakpointsPath(cmd))
		fmt.Println("Debugger disabled.")
		return nil
	},
}

var debugActivitiesCmd = &cobra.Command{
	Use:   "activities <Module.Microflow>",
	Short: "List a microflow's activities with the object IDs you can break on",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		if project == "" {
			return fmt.Errorf("--project (-p) is required to read the microflow")
		}
		acts, kind, err := resolveFlowActivities(project, args[0])
		if err != nil {
			return err
		}
		if len(acts) == 0 {
			fmt.Printf("No objects found in %s\n", args[0])
			return nil
		}
		fmt.Printf("Activities in %s (%s):\n\n", args[0], kind)
		fmt.Printf("  %-4s %-22s %-38s %s\n", "#", "Type", "Object ID", "Caption")
		for _, a := range acts {
			fmt.Printf("  %-4d %-22s %-38s %s\n", a.Index, a.Type, a.ObjectID, a.Caption)
		}
		fmt.Println("\nBreak with: mxcli debug break " + args[0] + " --activity '#<n>'  (or a caption substring)")
		return nil
	},
}

var debugBreakCmd = &cobra.Command{
	Use:   "break <Module.Microflow> --activity <#n|caption>",
	Short: "Set a breakpoint on a microflow activity, resolved by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		if project == "" {
			return fmt.Errorf("--project (-p) is required to resolve the activity")
		}
		selector, _ := cmd.Flags().GetString("activity")
		if selector == "" {
			return fmt.Errorf("--activity is required (an '#<index>' or caption substring); run 'mxcli debug activities %s' to see them", args[0])
		}
		condition, _ := cmd.Flags().GetString("if")

		acts, kind, err := resolveFlowActivities(project, args[0])
		if err != nil {
			return err
		}
		act, err := matchActivity(acts, selector)
		if err != nil {
			return err
		}

		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		if err := c.AddBreakpoint(args[0], act.ObjectID, condition, kind == flowNanoflow); err != nil {
			return err
		}

		// Record it locally for 'breaks' (the runtime has no list call).
		label := act.Caption
		if label == "" {
			label = fmt.Sprintf("%s#%d", act.Type, act.Index)
		}
		bpPath := breakpointsPath(cmd)
		bps, _ := loadBreakpoints(bpPath)
		bps = upsertBreakpoint(bps, localBreakpoint{Microflow: args[0], Activity: label, ObjectID: act.ObjectID, Condition: condition})
		if err := saveBreakpoints(bpPath, bps); err != nil {
			fmt.Printf("  (warning: could not record breakpoint locally: %v)\n", err)
		}

		fmt.Printf("Breakpoint set on %s → %s (%s)\n", args[0], label, act.ObjectID)
		if condition != "" {
			fmt.Printf("  condition: %s\n", condition)
		}
		fmt.Println("Any execution that reaches it — including a browser request — pauses until 'continue' (a later slice) or 'mxcli debug disable'.")
		return nil
	},
}

var debugUnbreakCmd = &cobra.Command{
	Use:   "unbreak <Module.Microflow> --activity <#n|caption>",
	Short: "Clear a breakpoint on a microflow activity",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, _ := cmd.Flags().GetString("project")
		if project == "" {
			return fmt.Errorf("--project (-p) is required to resolve the activity")
		}
		selector, _ := cmd.Flags().GetString("activity")
		if selector == "" {
			return fmt.Errorf("--activity is required (an '#<index>' or caption substring)")
		}
		acts, _, err := resolveFlowActivities(project, args[0])
		if err != nil {
			return err
		}
		act, err := matchActivity(acts, selector)
		if err != nil {
			return err
		}
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		if err := c.RemoveBreakpoint(act.ObjectID); err != nil {
			return err
		}
		bpPath := breakpointsPath(cmd)
		bps, _ := loadBreakpoints(bpPath)
		bps = removeBreakpoint(bps, act.ObjectID)
		_ = saveBreakpoints(bpPath, bps)
		fmt.Printf("Breakpoint cleared on %s (%s)\n", args[0], act.ObjectID)
		return nil
	},
}

var debugBreaksCmd = &cobra.Command{
	Use:   "breaks",
	Short: "List the breakpoints mxcli has set this session (name → object ID)",
	RunE: func(cmd *cobra.Command, args []string) error {
		bps, err := loadBreakpoints(breakpointsPath(cmd))
		if err != nil {
			return err
		}
		if len(bps) == 0 {
			fmt.Println("No breakpoints recorded. Set one with 'mxcli debug break <Module.Flow> --activity …'.")
			return nil
		}
		fmt.Println("Breakpoints set this session (mxcli's view):")
		for _, b := range bps {
			line := fmt.Sprintf("  %s → %s (%s)", b.Microflow, b.Activity, b.ObjectID)
			if b.Condition != "" {
				line += " if " + b.Condition
			}
			fmt.Println(line)
		}
		return nil
	},
}

var debugPausedCmd = &cobra.Command{
	Use:   "paused",
	Short: "Show microflows currently paused at a breakpoint, with their variables",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		flows, raw, events, err := allPausedFlows(c)
		if err != nil {
			return err
		}
		if len(flows) == 0 {
			fmt.Println("No microflows or nanoflows are paused.")
			return nil
		}
		fmt.Printf("%d paused flow(s):\n", len(flows))
		for _, f := range flows {
			fmt.Printf("  %s  (debug_id: %s)\n", f.Microflow, f.DebugID)
		}
		fmt.Println("\nMicroflow state (get_paused_microflows):")
		printJSON(raw)
		// A paused nanoflow's variables live in the poll_events payload, not in
		// get_paused_microflows — print it too when it carries paused entries.
		if len(extractPausedFromEvents(events)) > 0 {
			fmt.Println("\nClient events (poll_events) — includes paused nanoflows:")
			printJSON(events)
		}
		return nil
	},
}

var debugInspectCmd = &cobra.Command{
	Use:   "inspect <variable> [--list] [--flow <debug_id>]",
	Short: "Inspect a variable of a paused microflow (use --list for a list variable)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		flow, _ := cmd.Flags().GetString("flow")
		debugID, err := resolveDebugID(c, flow)
		if err != nil {
			return err
		}
		asList, _ := cmd.Flags().GetBool("list")
		var raw []byte
		if asList {
			raw, err = c.GetList(debugID, args[0])
		} else {
			raw, err = c.GetObject(debugID, args[0])
		}
		if err != nil {
			return err
		}
		printJSON(raw)
		return nil
	},
}

var debugStepCmd = &cobra.Command{
	Use:       "step [over|into|out] [--flow <debug_id>]",
	Short:     "Advance a paused microflow one step (default: over)",
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"over", "into", "out"},
	RunE: func(cmd *cobra.Command, args []string) error {
		kind := "over"
		if len(args) == 1 {
			kind = args[0]
		}
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		flow, _ := cmd.Flags().GetString("flow")
		debugID, err := resolveDebugID(c, flow)
		if err != nil {
			return err
		}
		if err := c.Step(kind, debugID); err != nil {
			return err
		}
		fmt.Printf("Stepped %s (debug_id: %s).\n", kind, debugID)
		return nil
	},
}

var debugContinueCmd = &cobra.Command{
	Use:   "continue [--all]",
	Short: "Resume a paused microflow (or all with --all)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := resolveDebuggerClient(cmd)
		if err != nil {
			return err
		}
		all, _ := cmd.Flags().GetBool("all")
		if err := c.Continue(all); err != nil {
			return err
		}
		if all {
			fmt.Println("Continued all paused microflows.")
		} else {
			fmt.Println("Continued.")
		}
		return nil
	},
}

// resolveDebugID returns the explicit --flow value, or auto-selects the single
// paused microflow. It errors (asking for --flow) when zero or several are paused
// so an action never targets the wrong flow.
func resolveDebugID(c *docker.DebuggerClient, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	flows, _, _, err := allPausedFlows(c)
	if err != nil {
		return "", err
	}
	switch {
	case len(flows) == 1 && flows[0].DebugID != "":
		return flows[0].DebugID, nil
	case len(flows) == 0:
		return "", fmt.Errorf("no paused microflows — nothing to act on")
	default:
		return "", fmt.Errorf("%d microflows are paused — pass --flow <debug_id> (see 'mxcli debug paused')", len(flows))
	}
}

// allPausedFlows merges the two sources of paused flows: get_paused_microflows
// (microflows) and poll_events (nanoflows, which do NOT appear in the former).
// Returns the merged summary plus both raw payloads for full-state printing.
func allPausedFlows(c *docker.DebuggerClient) (flows []pausedFlowSummary, paused, events []byte, err error) {
	paused, err = c.PausedMicroflows()
	if err != nil {
		return nil, nil, nil, err
	}
	flows = extractPausedFlows(paused)
	// poll_events is best-effort: a runtime that lacks it shouldn't break `paused`.
	if ev, evErr := c.PollEvents(); evErr == nil {
		events = ev
		for _, f := range extractPausedFromEvents(ev) {
			flows = appendUniqueFlow(flows, f)
		}
	}
	return flows, paused, events, nil
}

// appendUniqueFlow appends f unless a flow with the same debug_id is already present.
func appendUniqueFlow(flows []pausedFlowSummary, f pausedFlowSummary) []pausedFlowSummary {
	for _, e := range flows {
		if e.DebugID == f.DebugID {
			return flows
		}
	}
	return append(flows, f)
}

// printJSON pretty-prints a raw JSON message, falling back to the raw bytes if it
// isn't valid JSON.
func printJSON(raw []byte) {
	var buf bytes.Buffer
	if json.Indent(&buf, raw, "", "  ") == nil {
		fmt.Println(buf.String())
		return
	}
	fmt.Println(string(raw))
}

// upsertBreakpoint replaces an existing entry for the same object ID or appends.
func upsertBreakpoint(bps []localBreakpoint, bp localBreakpoint) []localBreakpoint {
	for i := range bps {
		if bps[i].ObjectID == bp.ObjectID {
			bps[i] = bp
			return bps
		}
	}
	return append(bps, bp)
}

// removeBreakpoint drops the entry with the given object ID.
func removeBreakpoint(bps []localBreakpoint, objectID string) []localBreakpoint {
	out := bps[:0]
	for _, b := range bps {
		if b.ObjectID != objectID {
			out = append(out, b)
		}
	}
	return out
}

// debugStateDir is the <projectDir>/.mxcli directory holding the session token
// and local breakpoint record (cwd/.mxcli when no project is given).
func debugStateDir(cmd *cobra.Command) string {
	project, _ := cmd.Flags().GetString("project")
	dir := "."
	if project != "" {
		dir = filepath.Dir(project)
	}
	return filepath.Join(dir, ".mxcli")
}

func breakpointsPath(cmd *cobra.Command) string {
	return filepath.Join(debugStateDir(cmd), "debug-breakpoints.json")
}

// resolveDebuggerClient builds a DebuggerClient from the flags/env, defaulting to
// a `run --local` runtime. The admin host is derived from --app-url so a remote
// runtime works too; the token is cached under <projectDir>/.mxcli/.
func resolveDebuggerClient(cmd *cobra.Command) (*docker.DebuggerClient, error) {
	appURL, _ := cmd.Flags().GetString("app-url")
	adminPort, _ := cmd.Flags().GetInt("admin-port")
	adminPass, _ := cmd.Flags().GetString("admin-pass")
	debugPass, _ := cmd.Flags().GetString("debug-pass")

	u, err := url.Parse(appURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid --app-url %q", appURL)
	}

	tokenPath := filepath.Join(debugStateDir(cmd), "debug-session.token")

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

	debugBreakCmd.Flags().String("activity", "", "Which activity: an '#<index>' (see 'debug activities') or a caption substring")
	debugBreakCmd.Flags().String("if", "", "Only pause when this Mendix expression is true (conditional breakpoint)")
	debugUnbreakCmd.Flags().String("activity", "", "Which activity: an '#<index>' or a caption substring")
	debugInspectCmd.Flags().String("flow", "", "Which paused microflow (debug_id); defaults to the only paused one")
	debugInspectCmd.Flags().Bool("list", false, "Inspect a list variable (uses get_list instead of get_object)")
	debugStepCmd.Flags().String("flow", "", "Which paused microflow (debug_id); defaults to the only paused one")
	debugContinueCmd.Flags().Bool("all", false, "Continue all paused microflows, not just one")

	debugCmd.AddCommand(debugStatusCmd)
	debugCmd.AddCommand(debugActivitiesCmd)
	debugCmd.AddCommand(debugBreakCmd)
	debugCmd.AddCommand(debugUnbreakCmd)
	debugCmd.AddCommand(debugBreaksCmd)
	debugCmd.AddCommand(debugPausedCmd)
	debugCmd.AddCommand(debugInspectCmd)
	debugCmd.AddCommand(debugStepCmd)
	debugCmd.AddCommand(debugContinueCmd)
	debugCmd.AddCommand(debugEnableCmd)
	debugCmd.AddCommand(debugDisableCmd)
	rootCmd.AddCommand(debugCmd)
}
