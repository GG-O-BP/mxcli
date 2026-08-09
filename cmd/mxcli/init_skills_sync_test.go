// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// embeddedSkillNames returns the skills this binary carries.
func embeddedSkillNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(skillsFS, "skills")
	if err != nil {
		t.Fatalf("reading embedded skills: %v", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no embedded skills; the embed directive is broken")
	}
	return names
}

// mxcli-formula1 §16: a project initialised on Monday still served Monday's
// skills from Tuesday's binary — the files are written once by `mxcli init` and
// nothing re-wrote them on upgrade. Stale guidance, no warning.
func TestSyncAIContextSkills_RefreshesStaleGuidance(t *testing.T) {
	dir := t.TempDir()
	names := embeddedSkillNames(t)

	// A project initialised by an older binary: the file exists, with content
	// that binary shipped.
	skillsDir := filepath.Join(dir, ".ai-context", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(skillsDir, names[0])
	if err := os.WriteFile(stale, []byte("# guidance from an older mxcli\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := syncAIContextSkills(dir)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if !res.Stale() {
		t.Fatal("the stale file was not detected")
	}
	if res.Total != len(names) {
		t.Errorf("Total = %d, want %d", res.Total, len(names))
	}

	// Every embedded skill now matches the binary, not just the one that existed.
	for _, n := range names {
		want, err := skillsFS.ReadFile("skills/" + n)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(skillsDir, n))
		if err != nil {
			t.Fatalf("%s missing after sync: %v", n, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s still disagrees with the binary", n)
		}
	}

	var out bytes.Buffer
	reportSkillSync(&out, res)
	if !strings.Contains(out.String(), names[0]) {
		t.Errorf("the refresh should name what it changed:\n%s", out.String())
	}
}

// This runs on every session start, so an up-to-date project must be silent and
// must not rewrite files — an mtime that moves on every session makes "when did
// this guidance last change" unanswerable.
func TestSyncAIContextSkills_CurrentProjectIsSilentAndUntouched(t *testing.T) {
	dir := t.TempDir()

	first, err := syncAIContextSkills(dir)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(first.Changed) != first.Total {
		t.Fatalf("a fresh project should write every skill: %d of %d", len(first.Changed), first.Total)
	}

	skillsDir := filepath.Join(dir, ".ai-context", "skills")
	probe := filepath.Join(skillsDir, first.Changed[0])
	before, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}

	second, err := syncAIContextSkills(dir)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if second.Stale() {
		t.Errorf("a current project reported changes: %v", second.Changed)
	}

	after, err := os.Stat(probe)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an unchanged skill was rewritten; mtime no longer means anything")
	}

	var out bytes.Buffer
	reportSkillSync(&out, second)
	if out.Len() != 0 {
		t.Errorf("the common path must be silent, got:\n%s", out.String())
	}
}

// The SessionStart bootstrap must actually run the sync, or the fix ships
// without the thing that triggers it.
func TestBootstrapScript_SyncsSkillsBeforeSetup(t *testing.T) {
	script := bootstrapScriptTemplate
	sync := strings.Index(script, "init --sync-skills")
	setup := strings.Index(script, "run --local --setup")
	switch {
	case sync < 0:
		t.Fatal("the bootstrap script does not refresh skills")
	case setup < 0:
		t.Fatal("the bootstrap script no longer runs setup")
	case sync > setup:
		t.Error("skills are refreshed after the exec'd setup, so never")
	}
	if !strings.Contains(script[sync:], "|| true") {
		t.Error("a failed skills refresh must not block the session")
	}
}
