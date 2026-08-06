// SPDX-License-Identifier: Apache-2.0

package theme

import (
	"errors"
	"strings"
	"testing"
)

func TestApplyBlock_AppendsToExistingFileWithoutLosingIt(t *testing.T) {
	existing := "// Mendix wrote this\n$brand-logo: false;\n"

	out, action, err := applyBlock(existing, "signal", "1", ":root { --x: 1; }", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionAdded {
		t.Errorf("action = %q, want %q", action, ActionAdded)
	}
	if !strings.Contains(out, "$brand-logo: false;") {
		t.Error("the project's own content must survive an apply")
	}
	if !strings.Contains(out, ":root { --x: 1; }") {
		t.Error("block body missing from output")
	}
}

func TestApplyBlock_CreatesEmptyFileWithoutLeadingBlankLine(t *testing.T) {
	out, action, err := applyBlock("", "signal", "1", "body", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionCreated {
		t.Errorf("action = %q, want %q", action, ActionCreated)
	}
	if strings.HasPrefix(out, "\n") {
		t.Errorf("unexpected leading blank line: %q", out)
	}
}

func TestApplyBlock_IsIdempotent(t *testing.T) {
	once, _, err := applyBlock("head\n", "signal", "1", "body", false)
	if err != nil {
		t.Fatal(err)
	}
	twice, action, err := applyBlock(once, "signal", "1", "body", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionUnchanged {
		t.Errorf("action = %q, want %q", action, ActionUnchanged)
	}
	if once != twice {
		t.Errorf("re-apply changed the file:\n--- first ---\n%s\n--- second ---\n%s", once, twice)
	}
}

func TestApplyBlock_ReplacesOwnBlockOnUpgrade(t *testing.T) {
	v1, _, err := applyBlock("head\n", "signal", "1", "old body", false)
	if err != nil {
		t.Fatal(err)
	}
	v2, action, err := applyBlock(v1, "signal", "2", "new body", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionUpdated {
		t.Errorf("action = %q, want %q", action, ActionUpdated)
	}
	if strings.Contains(v2, "old body") {
		t.Error("stale block body left behind")
	}
	if !strings.Contains(v2, "new body") || !strings.Contains(v2, "head") {
		t.Errorf("unexpected output:\n%s", v2)
	}
	if strings.Count(v2, beginMarker) != 1 {
		t.Errorf("block duplicated instead of replaced:\n%s", v2)
	}
}

// The guard this whole scheme exists for: once a human has edited inside the
// fence, mxcli must not overwrite it. Without the digest check the edit is
// silently discarded on the next `theme apply` or `mxcli new`-style re-run.
func TestApplyBlock_RefusesToClobberLocalEdits(t *testing.T) {
	generated, _, err := applyBlock("", "signal", "1", "--brand-primary: #0f6e6b;", false)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(generated, "#0f6e6b", "#ff0000", 1)
	if edited == generated {
		t.Fatal("test setup failed to modify the block")
	}

	out, action, err := applyBlock(edited, "signal", "1", "--brand-primary: #0f6e6b;", false)
	var modified *ErrBlockModified
	if !errors.As(err, &modified) {
		t.Fatalf("err = %v, want ErrBlockModified", err)
	}
	if action != ActionSkipped {
		t.Errorf("action = %q, want %q", action, ActionSkipped)
	}
	if out != edited {
		t.Error("file must be left exactly as the user wrote it")
	}
	if !strings.Contains(out, "#ff0000") {
		t.Error("user's edit was lost")
	}
}

func TestApplyBlock_ForceOverwritesLocalEdits(t *testing.T) {
	generated, _, _ := applyBlock("", "signal", "1", "original", false)
	edited := strings.Replace(generated, "original", "hand written", 1)

	out, action, err := applyBlock(edited, "signal", "1", "original", true)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionUpdated {
		t.Errorf("action = %q, want %q", action, ActionUpdated)
	}
	if strings.Contains(out, "hand written") {
		t.Error("--force should have replaced the edited block")
	}
}

func TestApplyBlock_LeavesOtherThemesAlone(t *testing.T) {
	withOther, _, _ := applyBlock("", "ledger", "1", "ledger body", false)
	out, _, err := applyBlock(withOther, "signal", "1", "signal body", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ledger body") || !strings.Contains(out, "signal body") {
		t.Errorf("both blocks should coexist:\n%s", out)
	}
}

func TestRemoveBlock_RestoresTheOriginalFile(t *testing.T) {
	original := "// Mendix wrote this\n$brand-logo: false;\n"
	applied, _, err := applyBlock(original, "signal", "1", "body", false)
	if err != nil {
		t.Fatal(err)
	}
	out, action, err := removeBlock(applied, "signal", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionRemoved {
		t.Errorf("action = %q, want %q", action, ActionRemoved)
	}
	if out != original {
		t.Errorf("remove did not restore the file byte for byte:\nwant %q\ngot  %q", original, out)
	}
}

func TestRemoveBlock_EmptiesAFileThatIsEntirelyOurs(t *testing.T) {
	applied, _, _ := applyBlock("", "signal", "1", "body", false)
	out, action, err := removeBlock(applied, "signal", false)
	if err != nil {
		t.Fatal(err)
	}
	if action != ActionRemoved || out != "" {
		t.Errorf("action = %q, out = %q; want removed and empty", action, out)
	}
}

func TestRemoveBlock_KeepsEditedBlocks(t *testing.T) {
	applied, _, _ := applyBlock("head\n", "signal", "1", "body", false)
	edited := strings.Replace(applied, "body", "my body", 1)

	out, action, err := removeBlock(edited, "signal", false)
	var modified *ErrBlockModified
	if !errors.As(err, &modified) {
		t.Fatalf("err = %v, want ErrBlockModified", err)
	}
	if action != ActionSkipped || out != edited {
		t.Error("an edited block must survive remove without --force")
	}
}

func TestRemoveBlock_NoBlockIsNotAnError(t *testing.T) {
	out, action, err := removeBlock("nothing here\n", "signal", false)
	if err != nil || action != ActionUnchanged || out != "nothing here\n" {
		t.Errorf("got (%q, %q, %v)", out, action, err)
	}
}
