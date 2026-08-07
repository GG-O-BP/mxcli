// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"

	"github.com/mendixlabs/mxcli/mdl/ast"
	"github.com/mendixlabs/mxcli/mdl/visitor"
)

// loopScopeViolations parses an MDL source containing exactly one microflow and
// returns the MDL053 messages the validator produces for it.
func loopScopeViolations(t *testing.T, src string) []string {
	t.Helper()
	prog, errs := visitor.Build(src)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	var msgs []string
	for _, stmt := range prog.Statements {
		mf, ok := stmt.(*ast.CreateMicroflowStmt)
		if !ok {
			continue
		}
		for _, v := range ValidateMicroflow(mf) {
			if v.RuleID == "MDL053" {
				msgs = append(msgs, v.Message)
			}
		}
	}
	return msgs
}

// TestLoopScope_IteratorUsedAfterLoop is the reported symptom: the loop
// iterator is referenced after `end loop`. Verified against mxbuild 11.12.1 as
// [CE0108] "Variable 'item' is defined but not in scope at this location."
func TestLoopScope_IteratorUsedAfterLoop(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Items: list of Sample.Thing)
begin
  loop $item in $Items
  begin
    change $item (Name = 'in');
  end loop;
  change $item (Name = 'after');
end;
`)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MDL053, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "$item") || !strings.Contains(msgs[0], "CE0108") {
		t.Errorf("message should name the variable and CE0108, got: %s", msgs[0])
	}
}

// A variable CREATED inside the loop body is loop-scoped too — same CE0108,
// confirmed against mxbuild 11.12.1.
func TestLoopScope_BodyVariableUsedAfterLoop(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Items: list of Sample.Thing)
begin
  loop $item in $Items
  begin
    $Inner = create Sample.Thing (Name = 'x');
  end loop;
  change $Inner (Name = 'after');
end;
`)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MDL053, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "$Inner") {
		t.Errorf("message should name $Inner, got: %s", msgs[0])
	}
}

// An inner loop's variable used in the outer loop body — after the inner
// `end loop` but still inside the outer one — is equally out of scope.
func TestLoopScope_InnerLoopVariableUsedInOuterBody(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Outer: list of Sample.Thing, $Inner: list of Sample.Thing)
begin
  loop $o in $Outer
  begin
    loop $i in $Inner
    begin
      change $i (Name = 'in');
    end loop;
    change $i (Name = 'outer body');
  end loop;
end;
`)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MDL053, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "$i") {
		t.Errorf("message should name $i, got: %s", msgs[0])
	}
}

// A reference from a branch that follows the loop is just as out of scope as a
// reference at the top level.
func TestLoopScope_ReferenceInBranchAfterLoop(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Items: list of Sample.Thing)
begin
  loop $item in $Items
  begin
    log info node 'Sample' 'x';
  end loop;
  if $Items != empty then
    change $item (Name = 'after');
  end if;
end;
`)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 MDL053, got %d: %v", len(msgs), msgs)
	}
}

// Everything that stays inside the loop body — including a nested branch and a
// nested loop reading the outer iterator — must not be reported.
func TestLoopScope_UsesInsideLoopAreClean(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Items: list of Sample.Thing, $Others: list of Sample.Thing)
begin
  declare $Total integer = 0;
  loop $item in $Items
  begin
    if $item/Name != empty then
      set $Total = $Total + 1;
    end if;
    loop $other in $Others
    begin
      change $other (Name = $item/Name);
    end loop;
    change $item (Name = 'still in scope');
  end loop;
  return $Total;
end;
`)
	if len(msgs) != 0 {
		t.Fatalf("expected no MDL053, got: %v", msgs)
	}
}

// The carry-out idiom — declare before the loop, assign inside, read after —
// is the recommended fix and must stay clean.
func TestLoopScope_CarryOutVariableIsClean(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($Items: list of Sample.Thing)
begin
  declare $LastName string = '';
  loop $item in $Items
  begin
    set $LastName = $item/Name;
  end loop;
  log info node 'Sample' $LastName;
end;
`)
	if len(msgs) != 0 {
		t.Fatalf("expected no MDL053, got: %v", msgs)
	}
}

// Two sibling loops REUSING an iterator name have no single owning body, so
// MDL053 must stay silent and leave the case to MDL052 (CE0111). Without this
// the first loop's own use of the name was reported as out of scope.
func TestLoopScope_DuplicateIteratorNameNotReported(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($A: list of Sample.Thing, $B: list of Sample.Thing)
begin
  loop $R in $A
  begin
    change $R (Name = 'a');
  end loop;
  loop $R in $B
  begin
    change $R (Name = 'b');
  end loop;
end;
`)
	if len(msgs) != 0 {
		t.Fatalf("expected no MDL053 for a duplicate iterator name (MDL052 owns it), got: %v", msgs)
	}
}

// Two sequential loops each using their own iterator: no cross-references, so
// nothing to report.
func TestLoopScope_SequentialLoopsAreClean(t *testing.T) {
	msgs := loopScopeViolations(t, `
create microflow Sample.MF ($A: list of Sample.Thing, $B: list of Sample.Thing)
begin
  loop $a in $A
  begin
    change $a (Name = 'a');
  end loop;
  loop $b in $B
  begin
    change $b (Name = 'b');
  end loop;
end;
`)
	if len(msgs) != 0 {
		t.Fatalf("expected no MDL053, got: %v", msgs)
	}
}
