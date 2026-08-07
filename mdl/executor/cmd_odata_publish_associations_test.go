// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"strings"
	"testing"
)

// mxcli-formula1 findings #10.4, and wider than reported: PublishAssociations
// false means "expose associations as an associated object id", and Mendix then
// requires the system ID attribute to be published as the entity key. MDL's
// `expose (Attr (KEY))` publishes an ordinary attribute, so the default of false
// failed the build with CE7375 for EVERY published service — persistent
// entities included, not only the non-persistable case that surfaced it.
//
// Verified on 11.12.1: the identical service (persistent entity, unique key)
// builds 0 errors with true and CE7375 with false.
func TestCreateODataService_DefaultsPublishAssociationsToTrue(t *testing.T) {
	for _, persistable := range []bool{true, false} {
		ctx, created, _ := publishCtx(t, "Row", persistable)
		if err := createODataService(ctx, publishStmt("Row")); err != nil {
			t.Fatal(err)
		}
		if !(*created).PublishAssociations {
			t.Errorf("persistable=%v: expected PublishAssociations to default to true", persistable)
		}
	}
}

// An explicit false is the author's choice — they have published an ID key, or
// they want to see what Mendix says. It is written as given.
func TestCreateODataService_ExplicitPublishAssociationsFalseIsHonoured(t *testing.T) {
	ctx, created, _ := publishCtx(t, "Row", true)
	stmt := publishStmt("Row")
	stmt.PublishAssociations = false
	stmt.PublishAssociationsSet = true
	if err := createODataService(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if (*created).PublishAssociations {
		t.Error("an explicit false must not be overridden by the default")
	}
}

// An explicit false over a non-persistable entity can never build, whatever the
// key is — publishing the ID of a non-persistable entity is forbidden. Warn
// rather than let CE7375 be the first anyone hears of it.
func TestCreateODataService_WarnsOnFalseWithNonPersistable(t *testing.T) {
	ctx, _, buf := publishCtx(t, "Row", false)
	stmt := publishStmt("Row")
	stmt.PublishAssociations = false
	stmt.PublishAssociationsSet = true
	if err := createODataService(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"PublishAssociations", "Probe.Row", "non-persistable", "CE7375"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning should mention %q, got: %s", want, out)
		}
	}
}

// The warning is for the unbuildable combination only: a persistent entity with
// an explicit false is a legitimate choice and must stay quiet.
func TestCreateODataService_NoWarningForPersistentWithFalse(t *testing.T) {
	ctx, _, buf := publishCtx(t, "Row", true)
	stmt := publishStmt("Row")
	stmt.PublishAssociations = false
	stmt.PublishAssociationsSet = true
	if err := createODataService(ctx, stmt); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Warning") {
		t.Errorf("no warning expected for a persistent entity, got: %s", buf.String())
	}
}
