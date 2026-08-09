// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mendixlabs/mxcli/cmd/mxcli/docker"
	"github.com/spf13/cobra"
)

// cmd_log.go exposes the runtime's log-node levels.
//
// This started life as a proposal for `mxcli odata trace`, to answer "what is my
// published OData resource actually being asked?" — a question that cost an
// afternoon because the URI is only visible at TRACE across the whole runtime.
// But `set_log_level` is subsystem-agnostic: it takes a list of nodes, and the
// runtime this was built against reports 57 of them. A per-subsystem command
// would have wrapped a generic primitive one subsystem at a time.
//
// So the command is generic, and the OData-specific knowledge (which node to
// raise) lives in the skill instead.

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Inspect and set the runtime's log levels",
	Long: `Read and change the log level of the running app's log nodes.

Logging inside the Mendix runtime is publish/subscribe: code publishes to a
named LogNode, and a subscriber records what it receives at or above that node's
level. Raising one node is how you see a subsystem's detail without drowning in
everything else — TRACE across the whole runtime is unusable on a busy app.

Levels, most to least severe:
  NONE  CRITICAL  ERROR  WARNING  INFO  DEBUG  TRACE

This talks to the M2EE admin API, so it needs a running app whose admin port is
reachable — typically one started by 'mxcli run --local'.

Examples:
  # What nodes exist, and at what level
  mxcli log list

  # Just the ones that look relevant
  mxcli log list --filter connectionbus

  # Raise one node, then put it back
  mxcli log set ConnectionBus_Queries TRACE
  mxcli log set ConnectionBus_Queries INFO

  # Several at once — one admin call, applied together
  mxcli log set ConnectionBus_Queries=TRACE Connector=DEBUG

  # A node that has not published yet (Mendix allows pre-registering a level)
  mxcli log set MyModule.MyNode TRACE --force`,
}

var logListCmd = &cobra.Command{
	Use:   "list",
	Short: "List log nodes and their current levels",
	Run: func(cmd *cobra.Command, args []string) {
		filter, _ := cmd.Flags().GetString("filter")
		asJSON, _ := cmd.Flags().GetBool("json")

		levels, err := docker.GetLogSettings(logAdminOptions(cmd))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n%s", err, logConnectionHint(cmd, err))
			os.Exit(1)
		}
		levels = docker.MatchLogNodes(levels, filter)

		if asJSON {
			out, _ := json.MarshalIndent(levels, "", "  ")
			fmt.Println(string(out))
			return
		}
		if len(levels) == 0 {
			if filter != "" {
				fmt.Printf("No log node matches %q.\n", filter)
			} else {
				fmt.Println("No log nodes reported.")
			}
			return
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NODE\tLEVEL\tSUBSCRIBER")
		for _, l := range levels {
			fmt.Fprintf(w, "%s\t%s\t%s\n", l.Node, l.Level, l.Subscriber)
		}
		w.Flush()
		fmt.Printf("\n(%d node/subscriber pair(s))\n", len(levels))
	},
}

var logSetCmd = &cobra.Command{
	Use:   "set <node> <level> | <node>=<level>...",
	Short: "Set the log level of one or more nodes",
	Long: `Set the level of one or more log nodes, in a single admin call.

Two spellings, because both read naturally:

  mxcli log set ConnectionBus_Queries TRACE
  mxcli log set ConnectionBus_Queries=TRACE Connector=DEBUG

An unknown node is refused, so a typo is an error rather than a setting that
silently never takes effect. Pass --force to set a level for a node that does
not exist yet — Mendix supports pre-registering one, which is how you capture a
subsystem's very first messages.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")

		levels, err := parseLogSetArgs(args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := docker.SetLogLevels(logAdminOptions(cmd), levels, force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n%s", err, logConnectionHint(cmd, err))
			os.Exit(1)
		}
		for _, l := range levels {
			up, _ := docker.NormalizeLogLevel(l.Level)
			fmt.Printf("%s -> %s\n", l.Node, up)
		}
	},
}

// parseLogSetArgs accepts either `<node> <level>` or repeated `<node>=<level>`.
// Mixing the two is refused rather than guessed at: `log set A=TRACE B DEBUG`
// has two readings and neither is obviously right.
func parseLogSetArgs(args []string) ([]docker.LogNodeLevel, error) {
	hasPairs := false
	for _, a := range args {
		if strings.Contains(a, "=") {
			hasPairs = true
		}
	}

	if !hasPairs {
		if len(args) != 2 {
			return nil, fmt.Errorf("expected '<node> <level>' or '<node>=<level>...', got %d argument(s)", len(args))
		}
		if _, err := docker.NormalizeLogLevel(args[1]); err != nil {
			return nil, err
		}
		return []docker.LogNodeLevel{{Node: args[0], Level: args[1]}}, nil
	}

	var out []docker.LogNodeLevel
	for _, a := range args {
		node, level, ok := strings.Cut(a, "=")
		if !ok || node == "" || level == "" {
			return nil, fmt.Errorf("%q is not '<node>=<level>' — do not mix the two forms in one command", a)
		}
		if _, err := docker.NormalizeLogLevel(level); err != nil {
			return nil, err
		}
		out = append(out, docker.LogNodeLevel{Node: node, Level: level})
	}
	return out, nil
}

func logAdminOptions(cmd *cobra.Command) docker.M2EEOptions {
	host, _ := cmd.Flags().GetString("admin-host")
	port, _ := cmd.Flags().GetInt("admin-port")
	pass, _ := cmd.Flags().GetString("admin-pass")
	return docker.M2EEOptions{Host: host, Port: port, Token: pass, Direct: true}
}

// logConnectionHint names the most likely cause when nothing is listening — but
// ONLY then. The runtime rejecting a request (an unknown node, a bad level) is
// not a connection problem, and telling someone to start an app they clearly
// already have running buries the sentence that matters.
func logConnectionHint(cmd *cobra.Command, err error) string {
	if !errors.Is(err, docker.ErrAdminUnreachable) {
		return ""
	}
	host, _ := cmd.Flags().GetString("admin-host")
	port, _ := cmd.Flags().GetInt("admin-port")
	return fmt.Sprintf("  Log levels come from a RUNNING app's admin API (%s:%d).\n"+
		"  Start one with 'mxcli run --local -p <app.mpr>', or point at another with --admin-host/--admin-port/--admin-pass.\n",
		host, port)
}

func init() {
	logCmd.PersistentFlags().String("admin-host", "127.0.0.1", "M2EE admin API host")
	logCmd.PersistentFlags().Int("admin-port", 8090, "M2EE admin API port")
	logCmd.PersistentFlags().String("admin-pass", envOr("MXCLI_ADMIN_PASS", "mxcli-local-dev"), "M2EE admin password")

	logListCmd.Flags().String("filter", "", "Only nodes whose name contains this (case-insensitive)")
	logListCmd.Flags().Bool("json", false, "Output as JSON")
	logSetCmd.Flags().Bool("force", false, "Allow a node the runtime does not know yet (it is refused otherwise)")

	logCmd.AddCommand(logListCmd)
	logCmd.AddCommand(logSetCmd)
	rootCmd.AddCommand(logCmd)
}
