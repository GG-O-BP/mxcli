---
title: Workflow / Microflow Syntax Alignment
status: draft
date: 2026-08-06
---

# Proposal: Workflow / Microflow Syntax Alignment

**Status:** Draft
**Date:** 2026-08-06

## Problem Statement

MDL spells the same concept differently depending on which document type you are
authoring. The clearest case is binding a parameter to a call, which has **three**
spellings today:

| Context | Syntax | Value |
|---|---|---|
| Microflow call | `call microflow M.F($p = $x, Level = 'INFO')` | expression |
| Page / widget datasource | `microflow M.F(Task: $Task)` | expression, **colon** |
| Workflow call | `call microflow M.F with (p = 'expr')` | **string literal only** |

This violates the project's own design rule. `.claude/skills/design-mdl-syntax.md`,
principle 2, states: *"Reuse existing patterns. Never create a second syntax for the
same concept."* The checklist item "No new keyword overloading" is likewise not met —
`annotation` attaches a note in microflows but is a separate (and, until recently,
project-corrupting) statement in workflows.

Beyond the developer-experience cost, this has a measurable quality cost.

### Defects cluster in the divergent surface

Every workflow defect reported across the two external test projects sits in a
workflow-only construct or its bespoke write path — code with no microflow
counterpart to inherit already-exercised machinery from:

| Finding | Construct | Severity |
|---|---|---|
| issuetracker #15 | `annotation '...'` statement | **model unloadable** (fixed: refused, MDL-WF04) |
| issuetracker #16 | `jump to <Task>` | target written as a comment (CE6680 + CE0495) |
| issuetracker #17 | `decision '<expr>'` | CE0117 "Error(s) in expression" |
| ledger #39 | workflow call-microflow storage name | CE6686 / CE0117 |
| ledger #41 | non-bare `with (...)` param names | rejected at check |

By contrast the constructs that *share* a code path with microflows — expressions,
qualified-name resolution, microflow references — have been comparatively stable.
The bespoke surface is where the defect density is, because it is the surface with
no second consumer keeping it honest.

### Who benefits

- **Authors and LLMs.** One pattern per concept means one example is enough to
  generate correct variants (design principle 3). Today an LLM that has learned
  microflow call syntax will produce an invalid workflow call.
- **Maintainers.** Fewer bespoke grammar rules to keep correct. The `with (...)`
  clause alone required a dedicated validator (`validate_workflow_refs.go`) to reject
  the qualified-name form the grammar still advertises.

## Scope: what is accidental vs. warranted

The useful test is **not** "are they different?" but *does the syntactic difference
track a semantic one?* Alignment is proposed only where it does not.

### Accidental (in scope)

| Divergence | Why it is accidental |
|---|---|
| `with (p = 'expr')` vs `(p = expr)` | Both write parameter mappings. The workflow form forces the value to be a `STRING_LITERAL`, so what is an expression everywhere else must be quoted — the direct cause of issuetracker #17's note that `'$workflowContext'` must be a quoted literal. |
| Grammar says `qualifiedName` for the param | The executor **already** prepends the microflow QN (`mfQN + "." + pm.Parameter`, `cmd_workflows_write.go`), and ledger #41 added a validator rejecting anything non-bare. The grammar advertises a form the tool refuses. |
| `comment 'x'` vs `@annotation 'x'` | Same intent, and the collision sent a user to the corrupting `annotation` statement. |
| No decorators at all in workflows | `workflowActivityStmt` has no `annotation*` prefix, so workflows cannot express `@position` — microflows can. The `annotation` rule is already domain-neutral (`MDLSettings.g4`), just not wired in. |
| Boolean `decision '<expr>'` | Writes `workflows.ExclusiveSplitActivity{Expression, outcomes}` — structurally what a microflow `if` already compiles to, but through a bespoke expression path (which is what fails CE0117). |

### Warranted (explicitly out of scope)

These have no microflow equivalent and should **not** be made to look like one:

- **Outcomes** on user tasks and call-microflow are first-class named model objects
  (`Workflows$UserTaskOutcome`), not flow labels. `if/else` cannot express them.
- **Boundary events**, **targeting** (users/groups, microflow/XPath), **due dates**,
  **multi user task** — workflow-only concepts.
- **No variables or assignment.** A workflow body has no `$var = ...`; there is only
  `$workflowContext`. This is the strongest argument *against* over-unification:
  making a workflow body look like imperative microflow code invites users to expect
  assignment that does not exist.
- **`{ }` vs `begin … end`.** Cosmetic. Changing it churns every existing script and
  all DESCRIBE round-trip tests for zero semantic gain.

**Guiding rule for this proposal:** *same spelling for the same concept; keep
distinct spellings where the semantics differ.* Not "make them look alike."

## BSON Structure

**No new BSON, and no change to any stored shape.** This is a front-end (grammar →
AST → visitor) change that routes into write paths that already exist and are already
exercised:

| Aligned construct | Existing write path | Status |
|---|---|---|
| Call-argument binding | `workflows.ParameterMapping{Parameter, Expression}` → `buildCallMicroflowTask` | exists; `Expression` is already a free-form string field |
| Activity note | `BaseWorkflowActivity.Annotation` → `appendActivityBaseFields` → nested `Workflows$Annotation{Description}` | exists; what `comment` already sets |
| Boolean decision | `workflows.ExclusiveSplitActivity{Expression, Outcomes}` → `buildExclusiveSplit` | exists; boolean outcomes already detected |

The one construct where placement genuinely is unresolved — the standalone
`annotation` statement — is deliberately **not** revived here. It has no valid
container (`Workflows$Annotation` takes only `Description` and attaches to a `Flow`;
`Workflows$FloatingAnnotation` is not a flow element either, and no struct in
`modelsdk/gen/workflows` owns a list of them). It stays refused under MDL-WF04 until
a Studio Pro reference establishes the correct container, per CLAUDE.md's rule on
unknown BSON shapes.

## Proposed MDL Syntax

All changes are **additive**. Every current form keeps parsing.

### 1. Call arguments (highest value)

```mdl
-- proposed (matches the microflow spelling; value is an expression)
call microflow Module.ACT_Escalate(Issue = $workflowContext, Level = 'High');

-- still accepted, unchanged
call microflow Module.ACT_Escalate with (Issue = '$workflowContext');
```

The bare `(...)` form takes an `expression`, so `$workflowContext` no longer has to
be smuggled through as a quoted string.

### 2. Activity notes and decorators

```mdl
-- proposed
@annotation 'Escalation path per policy 4.2'
user task Triage 'Triage the issue' page Module.TaskPage
  outcomes 'Done' { } 'Reject' { };

-- still accepted, unchanged
user task Triage 'Triage the issue' page Module.TaskPage comment 'Escalation path…'
  outcomes 'Done' { } 'Reject' { };
```

Wiring the shared `annotation*` prefix into `workflowActivityStmt` also makes
`@position(x, y)` available to workflows, which currently cannot express layout at
all. (Honouring `@position` is a follow-up, not part of this proposal — see Open
Questions.)

### 3. Boolean decisions

```mdl
-- proposed sugar for a two-outcome boolean decision
if $workflowContext/Priority = Module.Priority.Critical then {
  call microflow Module.ACT_Escalate(Issue = $workflowContext);
} else {
  call microflow Module.ACT_Normal(Issue = $workflowContext);
}

-- still accepted, unchanged
decision 'Priority check'
  outcomes true -> { … } false -> { … };
```

Enumeration decisions keep the `decision … outcomes 'Value' -> { }` form — they map
to `EnumerationValueConditionOutcome`, which `if/else` cannot express.

### Rejected alternative: unify on `comment` / SQL-shaped

ADR-0003 says MDL is SQL-shaped, and `COMMENT` is SQL-native, so the opposite
direction is defensible: add `comment '...'` to microflows and treat decorators as
legacy. Rejected because (a) it touches the far larger microflow surface, (b) it
fights the established decorator family (`@position`, `@caption`, `@anchor`) whose
purpose — presentation metadata held out-of-band from the logic — is exactly what an
activity note is, and (c) it is a breaking change in spirit for the more heavily used
document type.

## Implementation Plan

Phased, each phase independently shippable and independently revertable.

### Phase 1 — call arguments

| File | Change |
|------|--------|
| `mdl/grammar/domains/MDLWorkflow.g4` | `workflowCallMicroflowStmt` / `workflowCallWorkflowStmt`: accept `LPAREN callArgumentList? RPAREN` as an alternative to `WITH LPAREN … RPAREN` |
| `mdl/visitor/visitor_workflow.go` | Build `ParameterMappings` from either form; expression form stores the rendered expression |
| `mdl/executor/validate_workflow_refs.go` | Message update: suggest the bare-paren form |
| `mdl/executor/cmd_workflows.go` | DESCRIBE emits the new canonical form |
| `mdl-examples/doctype-tests/` | Cover both spellings |

### Phase 2 — decorators on workflow activities

| File | Change |
|------|--------|
| `mdl/grammar/domains/MDLWorkflow.g4` | `workflowActivityStmt : annotation* …` (rule already exists in `MDLSettings.g4`) |
| `mdl/visitor/visitor_workflow.go` | Map `@annotation` → the activity's `Annotation` property; ignore unknown decorators with a check-time warning |
| `.claude/skills/mendix/write-workflows.md` | Document `@annotation`; keep `comment` documented as the alias |

### Phase 3 — `if/else` sugar for boolean decisions

| File | Change |
|------|--------|
| `mdl/grammar/domains/MDLWorkflow.g4` | `workflowIfStmt : IF expression THEN LBRACE workflowBody RBRACE (ELSE LBRACE workflowBody RBRACE)?` |
| `mdl/visitor/visitor_workflow.go` | Lower to `WorkflowDecisionNode` with `true`/`false` outcomes — no new executor or writer code |

### Canonical form in DESCRIBE

DESCRIBE must emit exactly one spelling or round-trip tests become ambiguous. Proposal:
emit the **new** form from the phase that introduced it, and add a one-time note to the
changelog, since DESCRIBE output is not a stability contract (it is regenerated).

## Version Compatibility

Not Mendix-version-gated. Every aligned construct writes BSON that today's code
already writes, for every supported Mendix version. No `sdk/versions/*.yaml` entry is
required.

Backward compatibility with existing **scripts** is total: all current forms continue
to parse and produce identical BSON.

## Test Plan

- `mdl-examples/doctype-tests/` — workflow examples exercising both spellings of each
  aligned construct, so the alias path stays covered.
- **BSON-equivalence tests** (the load-bearing ones): assert that the old and new
  spellings of the same workflow produce byte-identical documents apart from
  regenerated UUIDs. This is what proves alignment is purely syntactic.
- `mdl-examples/bug-tests/it-15-workflow-annotation-refused.fail.mdl` — unchanged;
  MDL-WF04 must keep firing for the standalone statement.
- Integration: `mx check` = 0 errors on a workflow authored entirely in the new
  spelling, on Mendix 11.12.1.
- Round-trip: `describe workflow` output re-executes cleanly.

## Open Questions

1. **Canonical DESCRIBE spelling.** Switching output changes every workflow round-trip
   fixture in one commit. Acceptable, or should DESCRIBE keep emitting the legacy form
   for a release while the new one is accepted on input?
2. **`@position` for workflows.** Wiring the decorator prefix makes `@position`
   *parseable* immediately; honouring it needs workflow layout support that does not
   exist. Warn-and-ignore, or reject unknown decorators until implemented?
3. **Should the page/widget `Name: value` colon form also converge?** It is the third
   spelling and out of scope here, but leaving it makes "one way to do each thing"
   still only two-thirds true. Possibly a follow-up proposal.
4. **Is `jump to` (issuetracker #16) in or out?** It is broken independently of
   alignment. Fixing it is a bug fix; whether its syntax should also change is a
   separate question this proposal does not answer.

## Related

- ADR-0003 — MDL is SQL-shaped (the constraint this proposal works within)
- `.claude/skills/design-mdl-syntax.md` — principle 2 ("never create a second syntax
  for the same concept") and the keyword-overloading anti-pattern
- `PROPOSAL_mdl_syntax_improvements_v2.md` — a broader, more radical restyling
  (proposes dropping `create`/`begin`/`end`); shares the "unified experience" goal but
  is not additive and is not assumed here
- `PROPOSAL_workflow_improvements.md` — ALTER WORKFLOW + cross-references; capability
  gaps rather than syntax shape, largely shipped
