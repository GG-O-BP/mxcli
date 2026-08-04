// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/mendixlabs/mxcli/mdl/executor"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/spf13/cobra"
)

// cmd_widget_sync.go is the CLI for reconciling stored widget instances against the
// widget packages installed in the project — the mxcli equivalent of Studio Pro's
// "Update all widgets" and `mx update-widgets`, without the latter's MPR v2 data loss.
//
// Currently read-only: it reports what would change. Applying is the next step.

var widgetSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Reconcile stored widget instances against the installed widget packages",
	Long: `Compare every stored pluggable-widget instance against the .mpk currently
installed in the project, and report the schema differences.

mxcli writes a widget instance correctly for the package installed at authoring
time. When that package is later upgraded, the stored instances go stale and
Mendix reports CE0463 "the definition of this widget has changed" on each one.
This command finds those instances.

It reconciles SCHEMA — properties the package added, dropped, or redefined. It
never changes a property value you set, and it never touches a widget whose .mpk
is not installed.

Examples:
  mxcli widget sync -p app.mpr --dry-run
  mxcli widget sync -p app.mpr --dry-run --widget com.mendix.widget.web.datagrid.Datagrid
  mxcli widget sync -p app.mpr --dry-run --page MyModule.Overview`,
	RunE: runWidgetSync,
}

func init() {
	widgetSyncCmd.Flags().StringP("project", "p", "", "Path to .mpr project file")
	widgetSyncCmd.Flags().Bool("dry-run", false, "Report what would change without writing")
	widgetSyncCmd.Flags().String("widget", "", "Only this widget type (full widget ID)")
	widgetSyncCmd.Flags().String("page", "", "Only this page or snippet (qualified name)")
	widgetSyncCmd.MarkFlagRequired("project")
	widgetCmd.AddCommand(widgetSyncCmd)
}

func runWidgetSync(cmd *cobra.Command, args []string) error {
	projectPath, _ := cmd.Flags().GetString("project")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	widgetID, _ := cmd.Flags().GetString("widget")
	page, _ := cmd.Flags().GetString("page")

	exec, logger := newLoggedExecutor("subcommand")
	defer logger.Close()
	defer exec.Close()

	prog, errs := visitor.Build(fmt.Sprintf("CONNECT LOCAL '%s';", visitor.QuoteString(projectPath)))
	if len(errs) > 0 {
		return fmt.Errorf("connect: %v", errs[0])
	}
	exec.SetQuiet(true)
	if err := exec.ExecuteProgram(prog); err != nil {
		return fmt.Errorf("connect to %s: %w", projectPath, err)
	}

	opts := executor.SyncOptions{WidgetID: widgetID, Container: page}

	if dryRun {
		plan, err := executor.PlanWidgetSync(exec.Backend(), projectPath, opts)
		if err != nil {
			return err
		}
		renderSyncPlan(plan)
		return nil
	}

	res, plan, err := executor.ApplyWidgetSync(exec.Backend(), projectPath, opts)
	if err != nil {
		return err
	}
	renderSyncPlan(plan)
	fmt.Printf("\nApplied: %d property change(s) on %d widget(s) in %d unit(s).\n",
		res.PropertiesFixed, res.WidgetsChanged, res.UnitsChanged)
	if n := len(res.Skipped); n > 0 {
		fmt.Printf("Not applied: %d add(s) — see `mxcli widget sync --help`.\n", n)
	}
	return nil
}

// renderSyncPlan prints the plan. Every property is named rather than counted:
// removing a property discards its stored value, and that is exactly what a user
// needs to see before it happens.
func renderSyncPlan(plan *executor.SyncPlan) {
	out := os.Stdout

	if len(plan.Unresolved) > 0 {
		fmt.Fprintln(out, "Widgets used in the model with no installed .mpk (skipped, never modified):")
		sort.Strings(plan.Unresolved)
		for _, id := range plan.Unresolved {
			fmt.Fprintf(out, "  %s\n", id)
		}
		fmt.Fprintln(out)
	}

	if plan.Empty() {
		fmt.Fprintln(out, "Every stored widget instance already matches its installed package. Nothing to do.")
		return
	}

	container := ""
	for _, w := range plan.Widgets {
		if w.Container != container {
			container = w.Container
			fmt.Fprintf(out, "\n%s\n", container)
		}
		fmt.Fprintf(out, "  %s  (%s %s)\n", w.Widget, shortName(w.WidgetID), w.PackageVer)
		for _, c := range w.Changes {
			fmt.Fprintf(out, "    %s %-24s %s\n", changeMarker(c.Kind), c.Key, c.Detail)
		}
	}

	fmt.Fprintf(out, "\n%d widget instance(s), %d property change(s) across %d container(s).\n",
		len(plan.Widgets), plan.TotalChanges(), countContainers(plan))
	fmt.Fprintln(out, "Read-only: nothing was written.")
}

func changeMarker(k executor.SyncChangeKind) string {
	switch k {
	case executor.SyncRemove:
		return "-"
	case executor.SyncAdd:
		return "+"
	default:
		return "~"
	}
}

func countContainers(plan *executor.SyncPlan) int {
	seen := map[string]bool{}
	for _, w := range plan.Widgets {
		seen[w.Container] = true
	}
	return len(seen)
}

func shortName(widgetID string) string {
	for i := len(widgetID) - 1; i >= 0; i-- {
		if widgetID[i] == '.' {
			return widgetID[i+1:]
		}
	}
	return widgetID
}
