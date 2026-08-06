// SPDX-License-Identifier: Apache-2.0

// cmd_theme.go - `mxcli theme` : apply mxcli's built-in default styling
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mendixlabs/mxcli/cmd/mxcli/theme"
	"github.com/mendixlabs/mxcli/mdl/visitor"
	"github.com/spf13/cobra"
)

var themeCmd = &cobra.Command{
	Use:   "theme",
	Short: "Apply mxcli's built-in default styling to a project",
	Long: `Apply mxcli's built-in default styling to a Mendix project.

Three themes ship in the binary: signal (the default — cool slate, one teal
signal colour), ledger (warm paper, hairline rules, serif headings) and console
(dark-first, geometric). Run 'mxcli theme list' to see them and
'mxcli theme show <name>' for a theme's palette and the files it writes.

A theme is a set of files under theme/ — the model (.mpr) is never touched, so
it hot-applies under 'mxcli run --local --watch' and cannot affect a build.
Atlas Core is left untouched too. Each theme is a palette of --mxt-* tokens in
theme/web/custom-variables.scss, a shared map wiring those onto ~60 Atlas
variables, and a partial imported from theme/web/main.scss (which compiles
last). Re-branding is one line: change --mxt-brand.

Every theme ships light and dark palettes. With --variant auto (the default) the
app follows the operating system before first paint and honours a theme-light or
theme-dark class on the root element; --variant light or dark bakes one palette.
Nothing in Mendix sets that class, so 'mxcli theme switcher install' adds a
toggle — that subcommand is the only one here that writes to the model.

Only one theme applies at a time: applying removes the previous one, because two
themes mapping the same Atlas variables would fight in the cascade.

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
		fmt.Printf("Default palette: %s (auto switches to %s)\n\n", t.DefaultVariant, t.AltVariant())
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
Applying a theme removes any previously applied one, because two themes mapping
the same Atlas variables would fight in the cascade.

--variant auto (the default) ships both palettes: the app follows the OS and
honours a theme-light / theme-dark class on the root element. --variant light or
dark bakes a single palette with no switching.

With no name, apply refreshes the theme the project already has, and falls back
to the default only when it has none.

Examples:
  mxcli theme apply -p app.mpr
  mxcli theme apply ledger -p app.mpr
  mxcli theme apply console -p app.mpr --variant dark
  mxcli theme apply signal -p app.mpr --dry-run
  mxcli theme apply signal -p app.mpr --force`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := themeProjectDir(cmd)
		if err != nil {
			return err
		}
		// A bare `apply` refreshes the theme the project already has; only a
		// project with none falls back to the default.
		name := theme.DefaultName
		if len(args) == 1 {
			name = args[0]
		} else if name, err = theme.Resolve(dir, theme.DefaultName); err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")
		variantFlag, _ := cmd.Flags().GetString("variant")
		variant, err := theme.ParseVariant(variantFlag)
		if err != nil {
			return err
		}

		res, err := theme.Apply(dir, name, theme.Options{Force: force, DryRun: dryRun, Variant: variant})
		printThemeResult(res, dryRun)
		if err != nil {
			return err
		}
		if !dryRun && res.Changed() {
			if variant == theme.VariantAuto {
				fmt.Printf("\nLight and dark follow the OS. Add 'mxcli theme switcher install -p <app.mpr>'\n" +
					"for a user-facing toggle.\n")
			}
			fmt.Printf("\nRun 'mxcli run --local --watch -p <app.mpr>' to see it; SCSS edits hot-apply.\n")
		}
		return nil
	},
}

var themeRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Remove a theme's generated blocks from a project",
	Long: `Remove a theme's generated blocks from a project.

With no name, the installed theme is read from the mxcli:theme markers in the
project; if it has no theme, that is an error rather than a silent no-op.

A block carrying local edits is reported and left alone unless --force is given.

Examples:
  mxcli theme remove -p app.mpr
  mxcli theme remove ledger -p app.mpr --dry-run`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := themeProjectDir(cmd)
		if err != nil {
			return err
		}
		// No fallback here. Defaulting to the built-in theme meant that on a
		// project themed with any other one, remove reported every file as
		// unchanged and exited 0 — leaving the theme fully installed.
		name := ""
		if len(args) == 1 {
			name = args[0]
		} else if name, err = theme.Resolve(dir, ""); err != nil {
			return err
		}
		force, _ := cmd.Flags().GetBool("force")
		dryRun, _ := cmd.Flags().GetBool("dry-run")

		res, err := theme.Remove(dir, name, theme.Options{Force: force, DryRun: dryRun})
		printThemeResult(res, dryRun)
		return err
	},
}

var themeSwitcherCmd = &cobra.Command{
	Use:   "switcher",
	Short: "Install a runtime light/dark switcher (this one does touch the model)",
	Long: `Install a runtime theme switcher.

Unlike 'theme apply', this writes to the model: three JavaScript actions and two
nanoflows. It has to. A theme's light/dark blocks key off a class on the root
element, Mendix ships the slot but nothing that sets it, and there is no
theme-level hook to run script before first paint — so an explicit user choice
has to come from something the client can execute.

The CSS still does most of the work: --variant auto already renders the right
palette before first paint by following the OS. The switcher only covers the
case where a user overrides that.

Use --print to see the MDL without running it.`,
}

var themeSwitcherInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Create the switcher actions and nanoflows in a module",
	Long: `Create the theme switcher's JavaScript actions and nanoflows.

Creates, in the module given by --module:

  ToggleAppTheme      flips light/dark, resolving "follow the OS" first, and
                      remembers the choice in localStorage
  SetAppTheme         sets a theme explicitly; pass "auto" to clear the override
  ApplyStoredTheme    re-applies a remembered choice
  ACT_ToggleTheme     a nanoflow a button can call

Then wire a button wherever it belongs, typically a layout or settings page:

  actionbutton btnTheme (caption: 'Theme', action: nanoflow <Module>.ACT_ToggleTheme)

A click flips the palette and remembers it. The class is set on <html>, so
popups and modals — rendered at <body>, outside any page container — follow too.

Reload behaviour: the app goes back to following the OS. Mendix has no page
on-load event to re-apply the stored value from, and the usual substitute (a
data view with a nanoflow data source) is not authorable by mxcli on either
engine yet. ApplyStoredTheme is installed and ready for it — wire it in Studio
Pro if the choice must survive a reload.

Use --print to see the MDL without running it.

Examples:
  mxcli theme switcher install -p app.mpr
  mxcli theme switcher install -p app.mpr --module Ops
  mxcli theme switcher install --print --module Ops`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		module, _ := cmd.Flags().GetString("module")
		script := theme.SwitcherMDL(module)

		if printOnly, _ := cmd.Flags().GetBool("print"); printOnly {
			fmt.Print(script)
			return nil
		}

		projectPath, _ := cmd.Flags().GetString("project")
		if projectPath == "" {
			return fmt.Errorf("--project is required (or use --print to see the MDL)")
		}
		if err := execThemeMDL(projectPath, script); err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(theme.SwitcherNextSteps(module))
		return nil
	},
}

// execThemeMDL runs a generated MDL script against a project, the same way
// `mxcli exec` runs one from a file.
func execThemeMDL(projectPath, script string) error {
	ex, logger := newLoggedExecutor("theme")
	defer logger.Close()
	defer ex.Close()

	connect := fmt.Sprintf("CONNECT LOCAL '%s';", visitor.QuoteString(projectPath))
	prog, errs := visitor.Build(connect)
	if len(errs) > 0 {
		return fmt.Errorf("connecting to %s: %v", projectPath, errs[0])
	}
	if err := ex.ExecuteProgram(prog); err != nil {
		return fmt.Errorf("connecting to %s: %w", projectPath, err)
	}

	prog, errs = visitor.Build(script)
	if len(errs) > 0 {
		return fmt.Errorf("parsing the generated switcher MDL: %v", errs[0])
	}
	return ex.ExecuteProgram(prog)
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
	themeApplyCmd.Flags().String("variant", string(theme.VariantAuto),
		"Light/dark behaviour: auto (follow the OS + honour a theme class), light, or dark")
	themeSwitcherInstallCmd.Flags().StringP("project", "p", "", "Path to the .mpr file")
	themeSwitcherInstallCmd.Flags().String("module", "MyFirstModule", "Module to create the actions in")
	themeSwitcherInstallCmd.Flags().Bool("print", false, "Print the MDL instead of running it")
	themeSwitcherCmd.AddCommand(themeSwitcherInstallCmd)
	themeCmd.AddCommand(themeListCmd, themeShowCmd, themeApplyCmd, themeRemoveCmd, themeSwitcherCmd)
	rootCmd.AddCommand(themeCmd)
}
