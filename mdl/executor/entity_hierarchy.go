// SPDX-License-Identifier: Apache-2.0

package executor

import (
	"sort"
	"strings"

	"github.com/mendixlabs/mxcli/sdk/domainmodel"
)

// userEntityBase is the Mendix entity whose specializations are "user entities".
// Its members are managed by the platform (login, blocking, password), so they do
// NOT belong in a specializing entity's access rule — see EntityMembers.
const userEntityBase = "System.User"

// EntityMember is one member of an entity's access surface: an attribute or an
// association, together with the qualified reference Mendix stores for it.
type EntityMember struct {
	Name string // bare member name, as written in GRANT
	// Ref is the reference stored in MemberAccess, qualified against the entity
	// that DECLARES the member — which is an ancestor for an inherited one.
	Ref          string
	Inherited    bool
	IsCalculated bool
}

// EntityMembers returns every member of an entity's access surface: its own
// attributes plus those inherited through the generalization chain, each carrying
// the reference Mendix expects in a MemberAccess entry.
//
// Two rules here are load-bearing, both established against `mx check` rather than
// inferred (mendixlabs/mxcli#758, #765):
//
//  1. An inherited member's reference is qualified against the entity that
//     DECLARES it, not the entity carrying the rule. Writing the child's name
//     produces CE1613 "The selected attribute ... no longer exists"; writing the
//     declaring entity's name validates clean. This is the same rule the
//     change-object writer needs (#451).
//
//  2. Members inherited from System.User are excluded. Mendix manages the platform
//     members of a user entity, and listing them turns a clean rule into CE0066 —
//     verified both on Mendix's own Administration.Account and on a fresh
//     specialization. Every other ancestor's members are REQUIRED: omitting the
//     six System.FileDocument members from a specializing entity's rule is CE0066
//     until they are all present.
//
// Ancestors that cannot be resolved (module not in the project) stop the walk; the
// members found so far are returned rather than nothing, so a partial model still
// produces a usable rule.
func EntityMembers(ctx *ExecContext, entityQN string) []EntityMember {
	var out []EntityMember
	seen := map[string]bool{}    // cycle guard
	claimed := map[string]bool{} // a child's member shadows the ancestor's

	for currentQN, depth := entityQN, 0; currentQN != ""; depth++ {
		if seen[currentQN] {
			break
		}
		seen[currentQN] = true

		// Stop before collecting System.User's own members: its specializations are
		// user entities, whose platform members Mendix owns.
		if depth > 0 && strings.EqualFold(currentQN, userEntityBase) {
			break
		}

		entity, ok := findEntityByQN(ctx, currentQN)
		if !ok {
			break
		}

		for _, attr := range entity.Attributes {
			if attr == nil || claimed[attr.Name] {
				continue
			}
			claimed[attr.Name] = true
			out = append(out, EntityMember{
				Name:         attr.Name,
				Ref:          currentQN + "." + attr.Name,
				Inherited:    depth > 0,
				IsCalculated: attr.Value != nil && attr.Value.Type == "CalculatedValue",
			})
		}
		currentQN = entity.GeneralizationRef
	}
	return out
}

// findEntityByQN resolves a qualified entity name through the backend.
func findEntityByQN(ctx *ExecContext, entityQN string) (*domainmodel.Entity, bool) {
	if ctx == nil || ctx.Backend == nil {
		return nil, false
	}
	parts := strings.SplitN(entityQN, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}
	mod, err := ctx.Backend.GetModuleByName(parts[0])
	if err != nil || mod == nil {
		return nil, false
	}
	dm, err := ctx.Backend.GetDomainModel(mod.ID)
	if err != nil || dm == nil {
		return nil, false
	}
	entity := dm.FindEntityByName(parts[1])
	if entity == nil {
		return nil, false
	}
	return entity, true
}

// unmatchedGrantMembers returns the members named in a GRANT that matched no
// attribute or association of the entity, in a stable order.
func unmatchedGrantMembers(readMembers, writeMembers []string, granted map[string]bool) []string {
	var unknown []string
	seen := map[string]bool{}
	for _, list := range [][]string{readMembers, writeMembers} {
		for _, name := range list {
			if name == "" || granted[name] || seen[name] {
				continue
			}
			seen[name] = true
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}
