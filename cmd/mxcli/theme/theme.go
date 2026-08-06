// SPDX-License-Identifier: Apache-2.0

// Package theme applies mxcli's built-in default styling to a Mendix project.
//
// A theme is a set of files dropped into the project's theme/ folder — no model
// (.mpr) changes at all — so it compiles through Mendix's normal SCSS chain, hot
// applies under `mxcli run --local --watch`, and is removed by deleting a block.
//
// Where the files go was settled against a real Mendix 11.13 project rather than
// assumed, and two placements matter:
//
//   - theme/web/custom-variables.scss is imported by *every* module's theme
//     source (once per module), so it holds declarations only — never rules.
//     Atlas maps these CSS custom properties onto its components and onto the
//     brand-aware pluggable widgets, which is why a token retune re-brands the
//     whole app for free.
//
//   - theme/web/main.scss compiles LAST — after Atlas Core and after every
//     module theme source — so the partial it imports can override any Atlas
//     rule without !important. This is also why a theme must not write to
//     themesource/<name>/: a theme source folder is only compiled when <name>
//     matches a real module in the model, so an invented folder is silently
//     dropped from the build.
package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultName is the theme applied when the user does not choose one.
const DefaultName = "signal"

// NoneName opts out of default styling, leaving Atlas untouched.
const NoneName = "none"

// FileSpec documents one file a theme writes, for `mxcli theme show`.
type FileSpec struct {
	Path    string `json:"path"`
	Mode    string `json:"mode"` // "block" or "verbatim"
	Purpose string `json:"purpose"`
}

// Theme is a named styling package embedded in the mxcli binary.
type Theme struct {
	Name        string     `json:"name"`
	Title       string     `json:"title"`
	Version     string     `json:"version"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Colorway    []string   `json:"colorway"`
	Files       []FileSpec `json:"files"`
}

// FileResult is what applying one file did.
type FileResult struct {
	Path   string
	Action Action
}

// Result is the outcome of an Apply or Remove.
type Result struct {
	Theme string
	Files []FileResult
}

// Changed reports whether anything on disk actually moved.
func (r *Result) Changed() bool {
	for _, f := range r.Files {
		if f.Action != ActionUnchanged && f.Action != ActionSkipped {
			return true
		}
	}
	return false
}

// Options tunes an Apply or Remove.
type Options struct {
	// Force overwrites blocks that carry local edits.
	Force bool
	// DryRun reports what would change without writing.
	DryRun bool
}

// List returns the embedded themes, ordered by name.
func List() ([]Theme, error) {
	entries, err := fs.ReadDir(assetsFS, assetsRoot)
	if err != nil {
		return nil, fmt.Errorf("reading embedded themes: %w", err)
	}
	var themes []Theme
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t, err := Get(e.Name())
		if err != nil {
			return nil, err
		}
		themes = append(themes, *t)
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Name < themes[j].Name })
	return themes, nil
}

// Get returns one embedded theme by name.
func Get(name string) (*Theme, error) {
	raw, err := assetsFS.ReadFile(path.Join(assetsRoot, name, "theme.json"))
	if err != nil {
		return nil, fmt.Errorf("unknown theme %q (run `mxcli theme list`)", name)
	}
	var t Theme
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("theme %q: malformed theme.json: %w", name, err)
	}
	if t.Name != name {
		return nil, fmt.Errorf("theme %q: theme.json declares name %q", name, t.Name)
	}
	return &t, nil
}

// Apply writes a theme's files into projectDir.
//
// projectDir is the folder holding the .mpr — the theme/ tree sits beside it.
func Apply(projectDir, name string, opts Options) (*Result, error) {
	t, err := Get(name)
	if err != nil {
		return nil, err
	}
	if err := requireMendixProject(projectDir); err != nil {
		return nil, err
	}

	res := &Result{Theme: name}
	root := path.Join(assetsRoot, name, "files")
	// Edited blocks are collected rather than thrown on the first hit: a user who
	// has hand-tuned two files should see both named once, not discover the second
	// only after dealing with the first.
	var skipped []error

	walkErr := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		target := filepath.Join(projectDir, filepath.FromSlash(rel))

		body, err := assetsFS.ReadFile(p)
		if err != nil {
			return err
		}

		var fr FileResult
		if isBlockFile(rel) {
			fr, err = applyBlockFile(target, rel, t, string(body), opts)
		} else {
			fr, err = applyVerbatimFile(target, rel, body, opts)
		}
		if err != nil {
			var modified *ErrBlockModified
			if !errors.As(err, &modified) {
				return err
			}
			skipped = append(skipped, err)
		}
		res.Files = append(res.Files, fr)
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, errors.Join(skipped...)
}

// Remove cuts a theme's blocks back out and deletes the files it owns outright.
func Remove(projectDir, name string, opts Options) (*Result, error) {
	t, err := Get(name)
	if err != nil {
		return nil, err
	}

	res := &Result{Theme: name}
	root := path.Join(assetsRoot, name, "files")
	var skipped []error

	walkErr := fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		target := filepath.Join(projectDir, filepath.FromSlash(rel))

		existing, readErr := os.ReadFile(target)
		if os.IsNotExist(readErr) {
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionUnchanged})
			return nil
		}
		if readErr != nil {
			return readErr
		}

		if !isBlockFile(rel) {
			if !opts.DryRun {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionRemoved})
			return nil
		}

		out, action, err := removeBlock(string(existing), t.Name, opts.Force)
		if err != nil {
			var modified *ErrBlockModified
			if !errors.As(err, &modified) {
				return err
			}
			modified.Path = rel
			skipped = append(skipped, modified)
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionSkipped})
			return nil
		}
		// A file that is entirely ours is deleted rather than left empty.
		if out == "" {
			if !opts.DryRun {
				if err := os.Remove(target); err != nil {
					return err
				}
			}
			res.Files = append(res.Files, FileResult{Path: rel, Action: ActionRemoved})
			return nil
		}
		if action != ActionUnchanged && !opts.DryRun {
			if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
				return err
			}
		}
		res.Files = append(res.Files, FileResult{Path: rel, Action: action})
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}
	pruneEmptyThemeDirs(projectDir, root)

	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].Path < res.Files[j].Path })
	return res, errors.Join(skipped...)
}

// pruneEmptyThemeDirs removes directories the theme introduced once its files
// are gone, so `theme remove` leaves no empty theme/web/mxcli-fonts/ behind.
// os.Remove is the guard: it refuses a directory that still holds anything the
// project put there.
func pruneEmptyThemeDirs(projectDir, root string) {
	var dirs []string
	_ = fs.WalkDir(assetsFS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() || p == root {
			return err
		}
		dirs = append(dirs, strings.TrimPrefix(p, root+"/"))
		return nil
	})
	// Deepest first, so a nested tree empties from the bottom up.
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, rel := range dirs {
		_ = os.Remove(filepath.Join(projectDir, filepath.FromSlash(rel)))
	}
}

func applyBlockFile(target, rel string, t *Theme, body string, opts Options) (FileResult, error) {
	existing, err := os.ReadFile(target)
	if err != nil && !os.IsNotExist(err) {
		return FileResult{}, err
	}

	out, action, err := applyBlock(string(existing), t.Name, t.Version, body, opts.Force)
	if err != nil {
		var modified *ErrBlockModified
		if errors.As(err, &modified) {
			modified.Path = rel
			return FileResult{Path: rel, Action: ActionSkipped}, modified
		}
		return FileResult{}, err
	}
	if action == ActionUnchanged || opts.DryRun {
		return FileResult{Path: rel, Action: action}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return FileResult{}, err
	}
	if err := os.WriteFile(target, []byte(out), 0o644); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: rel, Action: action}, nil
}

func applyVerbatimFile(target, rel string, body []byte, opts Options) (FileResult, error) {
	existing, err := os.ReadFile(target)
	switch {
	case err == nil && string(existing) == string(body):
		return FileResult{Path: rel, Action: ActionUnchanged}, nil
	case err != nil && !os.IsNotExist(err):
		return FileResult{}, err
	}

	action := ActionCreated
	if err == nil {
		action = ActionUpdated
	}
	if opts.DryRun {
		return FileResult{Path: rel, Action: action}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return FileResult{}, err
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return FileResult{}, err
	}
	return FileResult{Path: rel, Action: action}, nil
}

// isBlockFile reports whether a file gets the marker treatment. Anything else
// (fonts, licences) is copied byte for byte.
func isBlockFile(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".scss", ".css", ".js":
		return true
	}
	return false
}

// requireMendixProject refuses to scatter theme files into a directory that is
// not a Mendix project. Applying to the wrong folder is silent otherwise: the
// files land, nothing compiles them, and the app just looks unstyled.
func requireMendixProject(dir string) error {
	if entries, err := filepath.Glob(filepath.Join(dir, "*.mpr")); err == nil && len(entries) > 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "themesource", "atlas_core")); err == nil {
		return nil
	}
	return fmt.Errorf("%s does not look like a Mendix project (no .mpr and no themesource/atlas_core)", dir)
}
