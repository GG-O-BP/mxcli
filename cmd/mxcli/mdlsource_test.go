// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mxcli-todo findings #5: `-` was taken literally as a filename, so a heredoc —
// the natural way to drive MDL from an agent or a shell script — failed with
// "open -: no such file or directory" and forced a temp file.
func TestReadMDLSource_Stdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	const script = "SHOW STRUCTURE DEPTH 1;\n"
	go func() {
		_, _ = w.WriteString(script)
		w.Close()
	}()

	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()

	got, err := readMDLSource(stdinPath)
	if err != nil {
		t.Fatalf("readMDLSource(%q): %v", stdinPath, err)
	}
	if string(got) != script {
		t.Errorf("got %q, want %q", got, script)
	}
}

func TestReadMDLSource_File(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.mdl")
	const script = "create module M;\n"
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readMDLSource(path)
	if err != nil {
		t.Fatalf("readMDLSource(%q): %v", path, err)
	}
	if string(got) != script {
		t.Errorf("got %q, want %q", got, script)
	}
}

func TestReadMDLSource_MissingFile(t *testing.T) {
	if _, err := readMDLSource(filepath.Join(t.TempDir(), "absent.mdl")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestMDLSourceLabel(t *testing.T) {
	if got := mdlSourceLabel(stdinPath); got != "<stdin>" {
		t.Errorf("label for stdin = %q, want <stdin>", got)
	}
	if got := mdlSourceLabel("script.mdl"); got != "script.mdl" {
		t.Errorf("label for a path = %q, want the path", got)
	}
}
