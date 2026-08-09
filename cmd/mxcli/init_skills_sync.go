// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// init_skills_sync.go keeps a project's .ai-context/skills/ in step with the
// mxcli binary that serves it.
//
// The skills are embedded in the binary and written once by `mxcli init`.
// Upgrading the binary therefore did nothing to them: a project initialised on
// Monday still served Monday's guidance from Tuesday's mxcli, with no warning —
// confirmed with a binary rebuilt at 12:05 beside skills stamped the previous
// day (mxcli-formula1 §16). Stale guidance is worse than missing guidance,
// because an agent reads it with the same confidence either way, and the whole
// point of shipping skills in the binary is that the two versions agree.
//
// These files are generated, never user-edited (the sources live in the mxcli
// repo), so refreshing is a copy, not a merge. The SessionStart bootstrap script
// runs it on every session — the moment before an agent would read them.

// skillSyncResult reports what a refresh did.
type skillSyncResult struct {
	Total   int      // skills the binary carries
	Changed []string // names whose on-disk content differed (added or updated)
}

// Stale reports whether anything on disk disagreed with the binary.
func (r skillSyncResult) Stale() bool { return len(r.Changed) > 0 }

// syncAIContextSkills rewrites <projectDir>/.ai-context/skills/ from the binary's
// embedded copies, reporting which files differed. Writing only the files that
// changed keeps mtimes meaningful, so "when did this guidance last move" stays
// answerable from the filesystem.
func syncAIContextSkills(projectDir string) (skillSyncResult, error) {
	var res skillSyncResult
	skillsDir := filepath.Join(projectDir, ".ai-context", "skills")

	entries, err := fs.ReadDir(skillsFS, "skills")
	if err != nil {
		return res, fmt.Errorf("reading embedded skills: %w", err)
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return res, fmt.Errorf("creating %s: %w", skillsDir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		want, err := skillsFS.ReadFile("skills/" + e.Name())
		if err != nil {
			return res, fmt.Errorf("reading embedded skill %s: %w", e.Name(), err)
		}
		res.Total++

		target := filepath.Join(skillsDir, e.Name())
		if have, readErr := os.ReadFile(target); readErr == nil && bytes.Equal(have, want) {
			continue
		}
		if err := os.WriteFile(target, want, 0o644); err != nil {
			return res, fmt.Errorf("writing %s: %w", target, err)
		}
		res.Changed = append(res.Changed, e.Name())
	}
	sort.Strings(res.Changed)
	return res, nil
}

// reportSkillSync prints a one-line summary, and nothing at all when the project
// was already current — this runs on every session start, so silence is the
// common case and the only acceptable one.
func reportSkillSync(w io.Writer, res skillSyncResult) {
	if !res.Stale() {
		return
	}
	fmt.Fprintf(w, "Refreshed %d of %d skill file(s) in .ai-context/skills/ to match this mxcli: %v\n",
		len(res.Changed), res.Total, res.Changed)
}
