# Marketplace module upgrade — snapshot data

Measurement data for
[`PROPOSAL_marketplace_module_upgrade.md`](../../PROPOSAL_marketplace_module_upgrade.md).

The question these snapshots exist to answer: **when Studio Pro updates an
installed marketplace module, what happens to the stored BSON — is it merged
into the existing elements, or replaced?** Studio Pro's Marketplace "Update" has
no `mx` equivalent and no documented merge semantics, so the only way to find out
is to snapshot a project, update a module in Studio Pro, and snapshot again.

## Subject

`test1-app` (`Test1App.mpr`), Mendix **11.13.0**, MPR v2, 369 units. Not under
version control, so a directory copy is the rollback.

| Module | Installed | Target | Why it is in the experiment |
|---|---|---|---|
| `Administration` | 4.3.2 | 4.5.0 | Has persistent entities and security; the interesting case |
| `DataWidgets` | 3.5.0 | 3.11.3 | Widget-only, no entities — a different upgrade shape |
| `MyFirstModule` | — | — | The **consumer** of `Administration` |

The consumer references were set up deliberately before the first snapshot:

- `MyFirstModule.MendixAccount` **generalizes** `Administration.Account`
  (inheritance is a harder dependency than an association)
- `MyFirstModule.MyFirstLogic` **retrieves** `Administration.Account`

And the conflict case is present: `Administration.AccountPasswordData` carries a
locally added attribute, `ExtraAttributeForTest`. An upgrade has to decide what
to do with it, and that decision is the whole of phase 2.

## Taking a snapshot

Always via the script, never by hand — a `--refs` mismatch between two runs
produces thousands of diff lines unrelated to the upgrade:

```bash
./snapshot.sh 01-before /path/to/Test1App.mpr
# ... update the module in Studio Pro ...
./snapshot.sh 02-after  /path/to/Test1App.mpr
diff -u 01-before/Administration.txt 02-after/Administration.txt
```

A third snapshot after updating to the **same** version again (`03-noop`) gives a
churn floor: anything that moves on a no-op update is noise to subtract from the
real comparison.

Reading the diff:

| Diff shape | Meaning |
|---|---|
| line identical | element untouched |
| same `id=`, changed `h=` | merged in place |
| changed `id=` | element replaced / renumbered |
| line added / removed | element added / deleted |

## Results

Three snapshots, two distinct Studio Pro operations:

| Label | State |
|---|---|
| `01-before` | Administration 4.3.2, DataWidgets 3.5.0, local edit present |
| `02-after` | after **updating the marketplace modules** |
| `03-after` | after **upgrading the widgets to their new definitions** |

### The module update is a replace, not a merge — but it transplants the GUID

Elements matched by name across `01-before` → `02-after`, in `Administration`:

| | Count |
|---|---|
| Matched by name | 94 |
| **`$ID` preserved** | **0** |
| `$ID` renumbered | 94 |
| Carrying a `GUID` | 9 |
| **`GUID` preserved** | **9 (all)** |

At unit level the same thing: 0 of 27 `Administration` units and 0 of 10
`DataWidgets` units kept their `$ID`, while 9 of each kept byte-identical
content. `Account` moved `562830a8…` → `1dee876e…` and kept
`guid=b16e49ea-91df-4caa-aed8-6ba4c4e133c5`.

So Studio Pro **discards the installed units and writes new ones, carrying the
`GUID` across name-keyed**. Neither existing description was right:
`PROPOSAL_marketplace_modules.md` calls it *"an ID-preserving merge"* — it is
neither ID-preserving nor a merge.

### Consumers are untouched, because they reference by name

`MyFirstModule` — which generalizes and retrieves `Administration.Account` — is
**identical** across the update apart from the project-wide unit count in the
header. All 7 units kept both `$ID` and content, even though `Account`'s `$ID`
changed underneath them.

This settles the §2 claim in the upgrade proposal. Renumbering happens, but it
does **not** break callers: cross-module references are qualified-name strings,
and every binary pointer is intra-unit and gets rewritten consistently
(`Associations[1]/ChildPointer` follows `Account` to its new `$ID`).

### The local modification was destroyed silently

`Administration.AccountPasswordData.ExtraAttributeForTest` is **gone** in
`02-after`. Not merged, not renamed, not reported as a conflict in the stored
model. Discounting index-path artefacts (see below), the update's real element
delta is exactly:

```
- Administration/DomainModel/Entities/AccountPasswordData/Attributes/ExtraAttributeForTest
+ Administration/ModuleSecurity/ModuleRoles/EditOwnDetails
+ Administration/ModuleSecurity/ModuleRoles/EditOwnPassword
+ Administration/ModuleSecurity/ModuleRoles/ReadOthersEmail
+ Administration/ModuleSecurity/ModuleRoles/ReadOthersFullName
+ Administration/ModuleSecurity/ModuleRoles/ReadOwnDetails
```

The five roles are genuine 4.5.0 content. The removal is the user's own work.

This inverts the framing of both proposals. mxcli's refusal to update a module
in place is not a gap relative to Studio Pro — on this evidence mxcli is
**safer**, and `marketplace diff` is not merely a precondition for a merge but
the only thing that would tell a user what an update is about to cost them.

### The widget upgrade is a true in-place edit

`02-after` → `03-after` is a completely different operation:

| | Result |
|---|---|
| Elements matched by name | 124 |
| `$ID` preserved | **124 (all)** |
| `GUID` preserved | 9 of 9 |
| Elements added / removed | 0 / 0 |
| Units with changed content | 7 of 28 |

The 7 are exactly the `Administration` pages carrying DataGrid2 widgets
(`ActiveSessions`, `RuntimeInstances`, `ScheduledEvents`, `Account_Edit`,
`Account_New`, `Account_Overview`, `MyAccount`). The `DataWidgets` module itself
is untouched (11 of 11 units byte-identical).

"Upgrade widget definitions" therefore rewrites widget **instances on pages**,
in place, preserving identity — nothing like the module update. Relevant to open
question 3 and to `PROPOSAL_widget_instance_reconciliation.md`.

### Version marker

`Administration/_Docs/v4.3.2` → `_Docs/v4.5.0`, and `DataWidgets` gained
`ClientActivities/Clear_Selection`. The `_Docs/v<version>` unit is a cheap
installed-version oracle, independent of the `AppStoreVersion` field that
`mx show-module-version` and `mxcli show modules` disagree about (open
question 4).

### Measurement artefact to discount

Unnamed nodes get index-keyed paths, so a widget property list that shifts from
`Properties[7]` to `Properties[6]` reads as 25 removals plus 25 additions. Of the
26 removed and 30 added elements in the module update, 25 of each are this
artefact. Normalising `[\d+]` → `[]` before comparing separates them, as the
delta above does. A future `marketplace diff` needs a stabler key for unnamed
widget nodes than the index.

## Baseline findings (01-before)

Measured before any update, and they reframe what the upgrade has to preserve.

**No ID pointer crosses a unit boundary.** Of 9,910 binary `$ID` references in
the project: 9,839 resolve inside the same unit, **0** resolve into a different
unit, 71 do not resolve. The unresolved ones are mostly identity fields rather
than references — 48 `DomainModel.GUID`, 16 `Microflow.StableId`, 2
`ProjectSecurity.GUID` — leaving 3 `DefaultPagePointer` and 2
`TypeParameterPointer` genuinely dangling. Those five are the only cross-unit
pointer candidates in the project and have not been explained yet.

**Cross-module references are qualified-name strings, not IDs:**

```
MyFirstModule/DomainModel   /Entities[1]/MaybeGeneralization/Generalization = "Administration.Account"
MyFirstModule/MyFirstLogic  /ObjectCollection/Objects[4]/Action/RetrieveSource/Entity = "Administration.Account"
```

**Domain-model elements carry a `GUID` separate from their `$ID`:**

```
E  Administration/DomainModel/Entities/Account  DomainModels$EntityImpl
   id=562830a8-ff20-4507-8d13-d435c347d2bd  guid=b16e49ea-91df-4caa-aed8-6ba4c4e133c5
```

### What this predicted, and how it held up

The baseline predicted that a renumber would not break consumers. The update
confirmed it: every `$ID` in the module changed and the consumer did not move.
The `GUID` guess also held — it is the one identity preserved across a full
replace — though its *role* (database mapping) is still inferred, not observed.

### What this contradicts

§2 of the proposal states that replacing a module's units *"would renumber every
entity in it, and every reference from the consuming app — an association to
`Administration.Account`, a security rule, a microflow retrieve — points at the
old `$ID`. The upgrade would break the callers."*

In this project those consumers point at the **name**, not the `$ID`. A `$ID`
renumber inside a module is therefore not automatically consumer-breaking. What
must survive an upgrade is the element **name**, the **`GUID`**, and each unit's
**internal** pointer consistency — not the `$ID`.

This is measured on one project at one Mendix version, and the role of `GUID` is
**not** established: that it is the database-mapping identity is a guess. The
proposal is deliberately not rewritten on this evidence alone — the correction
belongs alongside the post-update measurement.

## Files

| File | Contents |
|---|---|
| `<label>/full.txt` | Every unit and named element in the project, no `--refs` |
| `<label>/Administration.txt` | The upgraded module, with `--refs` |
| `<label>/DataWidgets.txt` | Widget-only module, with `--refs` |
| `<label>/MyFirstModule.txt` | The consumer, with `--refs` |

Generated by [`scripts/mprsnapshot`](../../../../scripts/mprsnapshot).
