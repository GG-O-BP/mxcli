// SPDX-License-Identifier: Apache-2.0

package docker

import "testing"

func TestDetectSessionID_Precedence(t *testing.T) {
	// Clear all sources; each subtest sets what it needs (t.Setenv restores after).
	clear := func(t *testing.T) {
		for _, k := range []string{"MXCLI_HUB_SESSION", "CLAUDE_CODE_REMOTE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
			t.Setenv(k, "")
		}
	}

	t.Run("explicit override wins", func(t *testing.T) {
		clear(t)
		t.Setenv("CLAUDE_CODE_REMOTE_SESSION_ID", "cse_remote")
		t.Setenv("MXCLI_HUB_SESSION", "override")
		if got := detectSessionID(); got != "override" {
			t.Errorf("got %q, want override", got)
		}
	})
	t.Run("remote session id preferred over per-run", func(t *testing.T) {
		clear(t)
		t.Setenv("CLAUDE_CODE_REMOTE_SESSION_ID", "cse_remote")
		t.Setenv("CLAUDE_CODE_SESSION_ID", "uuid")
		if got := detectSessionID(); got != "cse_remote" {
			t.Errorf("got %q, want cse_remote", got)
		}
	})
	t.Run("falls back to per-run id", func(t *testing.T) {
		clear(t)
		t.Setenv("CLAUDE_CODE_SESSION_ID", "uuid")
		if got := detectSessionID(); got != "uuid" {
			t.Errorf("got %q, want uuid", got)
		}
	})
	t.Run("empty when none set", func(t *testing.T) {
		clear(t)
		if got := detectSessionID(); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

func TestDetectHubMeta_FillsSession(t *testing.T) {
	t.Setenv("MXCLI_HUB_SESSION", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_REMOTE_SESSION_ID", "cse_web")

	m := DetectHubMeta("/tmp/App.mpr", HubMeta{})
	if m.Session != "cse_web" {
		t.Errorf("Session = %q, want cse_web", m.Session)
	}

	// An explicit override on the meta is not clobbered by the env.
	m2 := DetectHubMeta("/tmp/App.mpr", HubMeta{Session: "explicit"})
	if m2.Session != "explicit" {
		t.Errorf("Session = %q, want explicit (override preserved)", m2.Session)
	}
}
