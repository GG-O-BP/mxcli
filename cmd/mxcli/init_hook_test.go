// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddSessionStartHook_Empty(t *testing.T) {
	s := map[string]any{}
	if !addSessionStartHook(s, "cmd A") {
		t.Fatal("expected a change on empty settings")
	}
	hooks := s["hooks"].(map[string]any)
	ss := hooks["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Fatalf("SessionStart len = %d, want 1", len(ss))
	}
}

func TestAddSessionStartHook_Idempotent(t *testing.T) {
	s := map[string]any{}
	cmd := sessionStartHookCommand()
	addSessionStartHook(s, cmd)
	// Re-adding the identical command is a no-op.
	if addSessionStartHook(s, cmd) {
		t.Error("expected no change when the current command is already present")
	}
	ss := s["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Errorf("SessionStart len = %d, want 1 (no duplicate)", len(ss))
	}
}

// A project written by an older mxcli inlined the whole command in the hook.
// Re-running init must REWRITE that entry, not add a second one that runs the
// same bring-up twice. (mxcli-todo findings #2)
func TestAddSessionStartHook_MigratesLegacyCommand(t *testing.T) {
	s := map[string]any{}
	addSessionStartHook(s, "test -x ./mxcli && ./mxcli run --local --setup --ensure-db -p App.mpr || true")

	want := sessionStartHookCommand()
	if !addSessionStartHook(s, want) {
		t.Fatal("expected the legacy hook to be migrated")
	}
	ss := s["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(ss) != 1 {
		t.Fatalf("SessionStart len = %d, want 1 (migrated in place, not duplicated)", len(ss))
	}
	inner := ss[0].(map[string]any)["hooks"].([]any)
	if got := inner[0].(map[string]any)["command"].(string); got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}

func TestAddSessionStartHook_PreservesExisting(t *testing.T) {
	// Existing unrelated settings + a different SessionStart hook must survive.
	s := map[string]any{
		"model": "opus",
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo hi"}}},
			},
			"PostToolUse": []any{map[string]any{"hooks": []any{}}},
		},
	}
	if !addSessionStartHook(s, "run --local --setup -p App.mpr") {
		t.Fatal("expected a change")
	}
	if s["model"] != "opus" {
		t.Error("unrelated top-level setting was dropped")
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["PostToolUse"]; !ok {
		t.Error("unrelated hook group was dropped")
	}
	ss := hooks["SessionStart"].([]any)
	if len(ss) != 2 {
		t.Errorf("SessionStart len = %d, want 2 (existing + new)", len(ss))
	}
}

func TestEnsureSessionStartHook_WritesFile(t *testing.T) {
	dir := t.TempDir()
	changed, err := ensureSessionStartHook(dir, "App.mpr")
	if err != nil || !changed {
		t.Fatalf("ensureSessionStartHook = (%v,%v), want (true,nil)", changed, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), sessionStartHookCommand()) {
		t.Errorf("settings.json missing the hook command:\n%s", data)
	}
	// The hook delegates to a committed script, which must exist, name the
	// project's .mpr, and be executable.
	scriptPath := filepath.Join(dir, filepath.Base(bootstrapScriptName))
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("bootstrap script not written: %v", err)
	}
	if !strings.Contains(string(script), "MPR='App.mpr'") {
		t.Errorf("bootstrap script does not name the project:\n%s", script)
	}
	if !strings.Contains(string(script), "releases/download/") {
		t.Error("bootstrap script must be able to fetch mxcli after a reap")
	}
	if info, err := os.Stat(scriptPath); err == nil && info.Mode().Perm()&0o100 == 0 {
		t.Errorf("bootstrap script is not executable: %v", info.Mode())
	}
	// Valid JSON round-trips.
	var check map[string]any
	if err := json.Unmarshal(data, &check); err != nil {
		t.Errorf("settings.json is not valid JSON: %v", err)
	}
	// Second call is idempotent (no change).
	changed, err = ensureSessionStartHook(dir, "App.mpr")
	if err != nil || changed {
		t.Errorf("second call = (%v,%v), want (false,nil)", changed, err)
	}
}

func TestEnsureSessionStartHook_InvalidJSONUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	_ = os.WriteFile(path, []byte("{ not json"), 0o644)
	changed, err := ensureSessionStartHook(dir, "App.mpr")
	if err == nil {
		t.Error("expected an error for invalid existing settings.json")
	}
	_ = changed // the script may be (re)written even when settings.json is not
	// The original content is preserved.
	data, _ := os.ReadFile(path)
	if string(data) != "{ not json" {
		t.Errorf("invalid settings.json was modified: %q", data)
	}
}
