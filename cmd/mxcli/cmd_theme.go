// SPDX-License-Identifier: Apache-2.0

// cmd_theme.go - `mxcli theme` : apply mxcli's built-in default styling
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mendixlabs/mxcli/cmd/mxcli/theme"
	"github.com/spf13/cobra"
)

var themeCmd = &cobra.Command{
	Use:   "theme",
	Short: "Apply mxcli's built-in default styling to a project",
	Long: `Apply mxcli's built-in default styling to a Mendix project.

A theme is a set of files written into the project's theme/ folder — the model
(.mpr) is never touched. Atlas Core is left untouched too: the theme is a token
block in theme/web/custom-variables.scss plus one partial imported from
theme/web/main.scss, so the project stays upgradable across Mendix releases and
re-brands by editing a single colour.

Every generated block is fenced between mxcli:theme markers whose digest records
what mxcli wrote. Edit inside a fence and a later apply refuses rather than
discarding your work; edit outside it and mxcli never touches your lines.

New projects get the default theme automatically — see 'mxcli new --theme'.`,
}

var themeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the built-in themes",
	RunE: func(cmd *cobra.Command, args []string) error {
		themes, err := theme.List()
		if err != nil {
			return err
		}
		for _, t := range themes {
			marker := " "
			if t.Name == theme.DefaultName {
				marker = "*"
			}
			fmt.Printf("%s %-10s %-10s %s\n", marker, t.Name, t.Title, t.Summary)
		}
		fmt.Printf("\n* = applied by default. Use 'mxcli new --theme none' to opt out.\n")
		return nil
	},
}

var themeShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show what a theme contains and which files it writes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		t, err := theme.Get(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("%s (%s) v%s\n\n%s\n\n%s\n\n", t.Title, t.Name, t.Version, t.Summary, t.Description)
		if len(t.Colorway) > 0 {
			fmt.Printf("Colorway: %s\n\n", strings.Join(t.Colorway, "  "))
		}
		fmt.Println("Files:")
		for _, f := range t.Files {
			fmt.Printf("  %-42s %-9s %s\n", f.Path, f.Mode, f.Purpose)
		}
		return nil
	},
}

var themeApplyCmd = &cobra.Command{
	Use:   "apply [name]",
	Short: "Apply a theme to an existing project",
	Long: `Apply a theme to an existing project.

Applying is idempotent: re-running replaces only mxcli's own generated blocks.
A block carrying local edits is reported and left alone unless --force is given.

Examples:
  mxcli theme apply -p app.mpr
  mxcli theme apply signal -p app.mpr --dry-run
  mxcli theme apply signal -p app.mpr --force`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := theme.DefaultName
		if len(args) == 1 {
			name = args[0]
		}
		dir, err := themeProjectDir(cmd)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		res, err := theme.Apply(dir, name, theme.Options{Force: force, DryRun: dryRun})
		printThemeResult(res, dryRun)
		if err != nil {
			return err
		}
		if !dryRun && res.Changed() {
			fmt.Printf("\nRun 'mxcli run --local --watch -p <app.mpr>' to see it; SCSS edits hot-apply.\n")
		}
		return nil
	},
}

var themeRemoveCmd = &cobra.Command{
	Use:           "remove [name]",
	Short:         "Remove a theme's generated blocks from a project",
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := theme.DefaultName
		if len(args) == 1 {
			name = args[0]
		}
		dir, err := themeProjectDir(cmd)
		if err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		res, err := theme.Remove(dir, name, theme.Options{Force: force, DryRun: dryRun})
		printThemeResult(res, dryRun)
		return err
	},
}

// themeProjectDir resolves the folder holding the .mpr — the theme/ tree sits
// beside it. Accepts -p pointing at either the .mpr or its directory.
func themeProjectDir(cmd *cobra.Command) (string, error) {
	p, _ := cmd.Flags().GetString("project")
	if p == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return wd, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs, nil
	}
	return filepath.Dir(abs), nil
}

func printThemeResult(res *theme.Result, dryRun bool) {
	if res == nil {
		return
	}
	verb := ""
	if dryRun {
		verb = " (dry run)"
	}
	fmt.Printf("Theme '%s'%s\n", res.Theme, verb)
	for _, f := range res.Files {
		fmt.Printf("  %-9s %s\n", f.Action, f.Path)
	}
}

func init() {
	for _, c := range []*cobra.Command{themeApplyCmd, themeRemoveCmd} {
		c.Flags().StringP("project", "p", "", "Path to the .mpr file or project directory")
		c.Flags().Bool("force", false, "Overwrite blocks that carry local edits")
		c.Flags().Bool("dry-run", false, "Report what would change without writing")
	}
	themeCmd.AddCommand(themeListCmd, themeShowCmd, themeApplyCmd, themeRemoveCmd)
	rootCmd.AddCommand(themeCmd)
}
