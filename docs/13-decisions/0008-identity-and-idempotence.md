# ADR-0008: Skip unchanged writes; never renumber element IDs in place

- **Status**: Accepted
- **Date**: 2026-08-10
- **Related**: [ADR-0005](0005-semantic-model-interface-currency.md) (semantic model as interface currency); `docs/11-proposals/PROPOSAL_marketplace_module_upgrade.md` §4–§6; `mxcli-formula1` FINDINGS §50; PR #125 (reverted)

## Context

Two goals arrived at the same underlying question and were nearly solved the same
wrong way.

1. **Version-control hygiene.** Re-running an MDL script against a project already
   in sync rewrites documents with different bytes every time. Three consecutive
   re-runs of one script gave three different tree hashes across 143 documents
   while the semantic content never moved. Studio Pro shows the project as
   modified; `git diff` cannot answer "did this script change anything"; two
   people running the same script commit different bytes; a `.mxunit` merge is not
   resolvable by hand.
2. **Marketplace module upgrade.** Updating an installed module is blocked, and
   `PROPOSAL_marketplace_modules.md` parked it as needing "a future ID-preserving
   merge".

Both look like *"make element IDs stable"*. That framing produced a change
(PR #125) that made projects **unopenable**, and it is wrong twice over.

### What is actually true about identity

Measured, not assumed (`docs/11-proposals/data/marketplace-upgrade/`):

- **The unit is the identity boundary.** Of 9,910 binary `$ID` pointers in a real
  project, **0 cross a unit boundary**. Cross-unit references are qualified-name
  strings. Nothing outside a unit can observe its IDs.
- **Studio Pro renumbers freely.** Its module update preserved **0 of 94**
  `$ID`s in `Administration` and **9 of 9** `GUID`s, and consumers — a module
  that both generalizes and retrieves `Administration.Account` — were untouched.
- **Within a unit, IDs are load-bearing.** Pointers reference them, and Mendix's
  loader resolves those references after the fact
  (`StreamingBsonUnitReader.ResolvePostponedProperties`).

So an `$ID` is *internal wiring*, with one exception: an entity attribute's ID is
consumed by the runtime's database synchronizer, where a fresh ID reads as
"attribute departed, new attribute added" and drops the column (fixed separately
in `06a9face`).

### Why the obvious fix failed

PR #125 carried stored `$ID`s onto a rebuilt document. It rewrote element IDs and
left the pointers that referenced them holding the discarded values:

```
KeyNotFoundException: The given key '553f4a64-…' was not present in the dictionary
  at StreamingBsonUnitReader.ResolvePostponedProperties()
```

Microflows and nanoflows corrupted; pages and navigation survived — because a
page's widget tree is containment and a microflow's is a graph. The surviving
document types were not evidence of safety, and a local test project containing
no microflow reported `mx check` clean.

The structural reason it could not have worked: pointers are **not** child
elements. `Microflows$SequenceFlow.OriginPointer` is a primitive property holding
an `element.ID` (`InitFromRaw` in `modelsdk/gen/microflows/types.go`), so a walk
over `ChildProperty`/`ChildListProperty` traverses the whole document and never
sees a reference. mxcli cannot currently enumerate reference-valued properties at
all.

### Why the write happens in the first place

`create or replace` does not reconcile. It rebuilds the document from the MDL and
overwrites unconditionally — no write path compares against what is stored — and
every sub-element in that rebuild is a new object whose ID comes from
`GenerateID()`, a random UUID. The output is a function of the script *and a
random source*, so byte stability was never reachable by any amount of care in
the builders.

## Decision

**1. Do not write a unit whose new content is semantically equal to the stored
content.** Compare before writing, at the single choke point, and skip when equal.

Equality is judged on a **canonical form**: the document with every element `$ID`
replaced by its index in a deterministic containment walk. The set of element IDs
comes from a containment walk, and any occurrence of one of those IDs anywhere in
the document is a reference by definition — so this needs no knowledge of *which
properties* are references, and works today.

The comparison is **biased toward writing**. If canonicalisation cannot decide,
it reports "different" and the write proceeds. A false *different* costs a
redundant write, which is today's behaviour; a false *equal* silently discards
the user's intent.

**2. Never renumber an element `$ID` inside a unit that is otherwise being
preserved.** A unit is rewritten wholesale or not at all. Any future operation
that wants to move IDs — an ID-preserving merge, seeded rebuilds, deterministic
derivation — is blocked until reference-valued properties are enumerable, and
must rewrite IDs and references in one atomic pass.

**3. Marketplace module upgrade is import-and-transplant-`GUID`, not a merge.**
Write the package's units wholesale and carry each element's `GUID` across by
name. Their internal pointers arrive consistent, nothing is renumbered in place,
and consumers reference by qualified name, so decision 2 is satisfied for free.

## Consequences

**Both goals are reachable without the dangerous machinery.** Neither
version-control hygiene nor module upgrade requires ID preservation, deterministic
derivation, or reference remapping. Those remain available for a later, narrower
purpose — a *smaller* diff when something genuinely changed — and are explicitly
not prerequisites.

**One policy has to change first, and it is sufficient.**
`microflow_write.go` registers `StableId` as a `FreshGUIDField`, minting a new GUID
on every microflow write. While that holds, a microflow always differs from itself
and decision 1 skips nothing for the document type that dominates a script-authored
app.

Measured on `mxcli-sudoku` — 412 units, MPR v2, an idempotent 30-document script
set, two identical re-runs (~61s):

```
units                412
  identical          386
  volatile-only       26     all 26 are microflows
  real differences     0
```

**Zero real differences at scale**, and the split falls exactly along document
type rather than arbitrarily. `StableId` is registered on `Microflows$Microflow`
only, and the result mirrors that precisely: of the 30 documents the scripts
touch, the 26 microflows are volatile-only while the nanoflow, both pages and the
navigation document are *identical*. So freezing `StableId` is not merely
necessary for decision 1 — on this evidence it is **sufficient**.

For one microflow across the two runs:

```
run a: canon=ec4c23b68471026a  masked=6bc0ff14154da8de
run b: canon=46db2776c892421c  masked=6bc0ff14154da8de
StableId run 1: 26LgMMVoiE+ex1dmtRbDHQ==
StableId run 2: KwVkeqX3gU+s/ncDaO/7qw==
```

Canon moves, masked does not, and the field itself is what moved.

**The zero is a measurement, not a blind spot.** The same probe was run against
the withdrawn `PreserveIDs` binary on the same project and reported **10 real
differences** — the 9 microflows and the 1 nanoflow in that script set, with the
page unchanged. That independently reproduces the microflows-corrupt /
pages-survive split, and demonstrates the probe can report non-zero on exactly
the failure this ADR exists to prevent.

**Verification has a standing shape.** `scripts/mprsnapshot -canon` emits per-unit
canonical digests; two runs plus a diff answers "how many units would be skipped"
without changing any write behaviour. Any claim about idempotence should cite that
measurement on a project that contains microflows.

**A green `mx check` is not evidence about the write path** unless the fixture
contains the constructs at risk. This is the direct lesson of PR #125 and belongs
in any future attempt's test plan.

### Known limit of the evidence

The measurements above cover microflows, nanoflows, pages and navigation — the
*internal wiring* class, where this ADR's argument is strongest. They do **not**
cover domain-model documents, because the probe needs scripts that apply cleanly
twice and the available domain scripts do not (`create module` and
`alter entity … add attribute` fail on a second run).

That leaves untested precisely the exception this ADR names: an entity attribute's
`$ID`, which the database synchronizer reads as identity. Two things are already
true there and should be checked rather than assumed when it is measured:

- `create or modify entity` **already reuses each retained attribute's existing ID
  by name** (findings #13, `06a9face`) — so the identity concern is addressed on
  that path, and a probe run over it would be confirming a fix rather than
  discovering one.
- `create or modify entity` still **rebuilds the entity from the statement alone**,
  so any attribute the statement omits is deleted. It is no longer silent — a
  non-blocking warning lists what is dropped (findings #24) — but a script used to
  probe idempotence must list every attribute or it will measure a real change it
  caused itself.

Closing this gap needs an idempotent domain-model script. Until then, decision 1's
safety argument for attributes rests on the identity fix above rather than on a
measurement.

## Alternatives considered

**Preserve stored IDs onto the rebuilt tree** (PR #125). Rejected: needs reference
remapping mxcli cannot express, and measured at ~1% churn reduction where it was
safe, because unnamed sub-elements dominate a page and cannot be matched by name.

**Derive IDs deterministically** from a stable seed, making the rebuild a pure
function. Attractive — pointers computed during the build are consistent by
construction, and `GenerateDeterministicID` already exists for System entities.
Rejected *as the first step*: unnamed elements have no stable seed but their
structural path, which changes exactly when a list shifts; adoption rewrites every
derived ID once; and attributes must still be seeded from storage. Decision 1
delivers the same user-visible outcome with none of that.

**Compare and skip on raw bytes.** Rejected: the rebuilt bytes always differ,
because the IDs are freshly random. Byte comparison would skip nothing.

**An element-level merge for marketplace upgrade.** Rejected on evidence: Studio
Pro does not merge, and §4 shows it does not need to. A merge would also require
in-place renumbering, which decision 2 forbids.
