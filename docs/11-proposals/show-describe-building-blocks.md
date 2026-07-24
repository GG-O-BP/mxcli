---
title: SHOW/DESCRIBE/USE Building Blocks
status: partial
related:
  - PR #16 (READ — SHOW/DESCRIBE + catalog)
  - PR #17 (READ — modelsdk ListBuildingBlocks + exec-safe example)
---

# Proposal: SHOW / DESCRIBE / USE Building Blocks

## Overview

**Document type:** `Pages$BuildingBlock` (NOT `Forms$BuildingBlock` — the reader now
does a `Pages$`-first, `Forms$`-fallback lookup)
**Prevalence:** every project ships ~39 out-of-the-box Atlas blocks (`Atlas_Web_Content`)
**Priority:** High — present in every project, reusable UI components

Building Blocks are reusable widget compositions that can be dragged onto pages in Studio Pro. They are structurally similar to Snippets but serve as templates rather than runtime components.

The Building-Block capability has three layers — **READ → INSTANTIATE → AUTHOR**:

| Capability | What | Status |
|---|---|---|
| **READ** — `SHOW`/`DESCRIBE BUILDING BLOCK`, `CATALOG.building_blocks` | discover + inspect a block's widget tree | ✅ **shipped** (PR #16/#17); validated live against a real Atlas app (40 blocks) |
| **INSTANTIATE** — `USE BUILDING BLOCK` | deep-copy a block onto a page | 📐 **specced below** (v1 in progress) |
| **AUTHOR** — `CREATE BUILDING BLOCK` | contribute a block back to the toolbox | ⬜ future |

The rest of this document: the (now-shipped) READ design for reference, then the
**USE BUILDING BLOCK follow-up slice**.

## What Already Exists

| Layer | Status | Location |
|-------|--------|----------|
| **Go type** | Yes | `sdk/pages/pages.go` — `BuildingBlock{Name, documentation, widget, TemplateID}` |
| **Parser** | Minimal | `sdk/mpr/parser_misc.go` line 165 — Name + Documentation only, no widgets |
| **Reader** | Yes | `ListBuildingBlocks()` in `sdk/mpr/reader_types.go` |
| **Generated metamodel** | Yes | Full struct in `generated/metamodel/types.go` |
| **AST** | No | — |
| **Executor** | No | — |
| **Grammar** | No | — |

## BSON Structure (from test projects)

```
Forms$BuildingBlock:
  Name: string
  documentation: string
  DisplayName: string
  Excluded: bool
  ExportLevel: string
  Platform: string ("Web" | "Native")
  TemplateCategory: string
  TemplateCategoryWeight: int32
  CanvasWidth: int32
  CanvasHeight: int32
  DocumentationUrl: string
  ImageData: binary (preview thumbnail)
  widgets: []*widget (same widget tree as pages)
```

## Proposed MDL Syntax

### SHOW BUILDING BLOCKS

```
show BUILDING BLOCKS [in module]
```

Output table columns:

| Qualified Name | Module | Name | Display Name | Platform | Category | Widgets |
|----------------|--------|------|--------------|----------|----------|---------|

### DESCRIBE BUILDING BLOCK

```
describe BUILDING BLOCK Module.Name
```

Output format (similar to DESCRIBE SNIPPET):

```
/**
 * A reusable card component
 */
-- Building Block: MyModule.CustomerCard
-- Display Name: Customer Card
-- Platform: Web
-- Category: Cards
BUILDING BLOCK MyModule.CustomerCard
{
  container
  {
    textbox $Name;
    textbox $Email;
  };
};
/
```

For the initial implementation, widget tree output can be simplified to show structure without full property details (same approach as DESCRIBE SNIPPET).

## Implementation Steps

### 1. Enhance Parser (sdk/mpr/parser_misc.go)

Extend `parseBuildingBlock()` to capture:
- `DisplayName`, `Platform`, `TemplateCategory`, `Excluded`, `ExportLevel`
- Widget tree parsing (reuse `parseWidgets()` from `parser_page.go`)

Update `BuildingBlock` struct in `sdk/pages/pages.go` to add `DisplayName`, `Platform`, `TemplateCategory`.

### 2. Add AST Types (mdl/ast/ast_query.go)

```go
ShowBuildingBlocks    // in ShowObjectType enum
DescribeBuildingBlock // in DescribeObjectType enum
```

### 3. Add Grammar Rules

```antlr
BUILDING: 'BUILDING';
BLOCK: 'BLOCK';
BLOCKS: 'BLOCKS';

// show BUILDING BLOCKS [in module]
// describe BUILDING BLOCK qualifiedName
```

### 4. Add Executor (mdl/executor/cmd_building_blocks.go)

- `showBuildingBlocks(moduleName string)` — table listing
- `describeBuildingBlock(name QualifiedName)` — MDL output with widget tree

The DESCRIBE handler can reuse the widget tree formatter from `cmd_pages_describe.go`.

### 5. Add Autocomplete

```go
func (e *Executor) GetBuildingBlockNames(moduleFilter string) []string
```

## Testing

- Create `mdl-examples/doctype-tests/17-building-block-examples.mdl`
- Verify against all 3 test projects

---

# Follow-up slice: `USE BUILDING BLOCK` (Instantiate)

The READ capability lets you *discover and inspect* a project's out-of-the-box Atlas
blocks. `USE BUILDING BLOCK` closes the loop: **deep-copy** a block's widget tree onto
a page in one line, instead of hand-mirroring its `DESCRIBE` output. This is the single
biggest lever for the `atlas-design` skill — it turns "discover → inspect → **mirror**"
into "discover → **use**".

## Syntax — mirrors `use fragment`, sourced from a persisted block

```mdl
use building block Atlas_Web_Content.Card            -- deep-copy the block's tree here
use building block Atlas_Web_Content.Card as cust_   -- prefix the copied widget names
```

It is a **page-body element** (valid anywhere `use fragment` is — page bodies, containers,
columns, placeholders), not a top-level statement. The only syntactic difference from
`use fragment X as p_` is the **qualified name** (`Module.Name`), because a building block
is a persisted `Pages$BuildingBlock` document rather than a script-scoped `define fragment`.

### Worked example

```mdl
create page Sales.CustomerOverview (title: 'Customers') {
  layoutgrid g1 {
    row r1 {
      column c1 (desktopwidth: 6) { use building block Atlas_Web_Content.Card as cust_ }
      column c2 (desktopwidth: 6) { use building block Atlas_Web_Content.Card as order_ }
    }
  }
}
```

Each `use` deep-copies the block's real tree (from `DESCRIBE Atlas_Web_Content.Card`):
`container (DesignProperties: ['Card style': on]) { dynamictext (Class: 'card-title', …) }`
→ `as cust_` yields `cust_container…` + `cust_text…`, `as order_` the parallel set.

## Semantics (match how Atlas blocks actually behave)

- **Deep copy, no live link** — identical to dragging a block onto a page in Studio Pro.
  After insertion the widgets are the page's own, freely editable.
- **Name-collision handling** via the `as <prefix>` rename (same as fragments).
- **Read-only source** — the block document is never mutated; `USE` only reads it.

## Configuration: afterwards (v1), inline sugar (v1.1) — never magic

A building block has **no parameter interface** — it is a raw widget-tree template, so
there is nothing to pass arguments *to*. You configure by editing the copied widgets.

- **v1 — configure afterwards** with the already-shipped `alter page` commands. This is
  the honest default (mirrors Studio Pro's drag-then-configure) and needs zero new
  machinery beyond the copy:
  ```mdl
  use building block Atlas_Web_Content.Card as cust_
  alter page Sales.CustomerOverview set cust_text22 (content: 'Customers');
  ```
- **v1.1 — optional inline override block** (sugar over `use` + immediate `alter`), scoped
  to the freshly-copied widgets, referencing the block's real internal names (from
  `DESCRIBE`):
  ```mdl
  use building block Atlas_Web_Content.Card as cust_ {
    set text22 (content: 'Customers')
  }
  ```
- **Maybe — a data-context shortcut** for the one config a block often can't work without
  (a `Master_Detail`/`List_Cards` dropped with no entity is inert):
  `use building block Atlas_Web_Content.List_Cards as prod_ over database Sales.Product`
  — this just resolves to "set the copied datasource host's source", not a slot.
- **Never — implicit slots.** Do not have the engine guess which widget is "the title" or
  "the datasource host". Atlas blocks don't declare roles; heuristic inference is fragile.
  Keep configuration **explicit** (by widget name), inline or post-hoc.

## Implementation (v1) — reuse the fragment-expansion machinery

Confirmed feasible; v1 in progress on `feature/use-building-block`.

1. **Grammar** — add `useBuildingBlockRef : USE BUILDING BLOCK qualifiedName (AS
   identifierOrKeyword)?` to `MDLPage.g4`, as an alternative everywhere `useFragmentRef`
   appears in the page body. (`BUILDING`/`BLOCK` tokens already exist from READ.)
2. **AST/visitor** — mirror `buildUseFragmentRef`: emit a sentinel
   `WidgetV3{Type: "USE_BUILDING_BLOCK", Name: "<Module.Name>", Properties: {Prefix}}`.
3. **Executor** — in `expandIfFragment` (the same hook that expands `USE_FRAGMENT`), add a
   branch that resolves the block (`ListBuildingBlocks` + hierarchy), reads its widgets via
   the READ path (`getBuildingBlockWidgetsFromRaw` → `GetRawUnit`), and converts them to
   `[]*ast.WidgetV3`, then applies the prefix and returns them for the normal builder to
   serialize.
4. **BSON → `[]*ast.WidgetV3` conversion** — rather than a fragile hand-written converter,
   **round-trip through the DESCRIBE renderer**: render the block's raw widgets to MDL
   (`outputWidgetMDLV3`), wrap in `define fragment __tmp as { … }`, re-parse with
   `visitor.Build`, and take `.Widgets`. This reuses the entire existing widget
   parser/visitor, so it handles every widget type DESCRIBE can emit and degrades to a
   clear parse error otherwise. (Verified: real Card DESCRIBE output re-parses cleanly.)

## Testing

- Executor mock test: a block's raw widgets → expanded (prefixed) into a page's widget list.
- Real end-to-end: `use building block Atlas_Web_Content.Card as cust_` on a real Atlas
  project, then `describe page` to confirm the prefixed card widgets landed, plus `mx check`.

## Later

- **v1.1** inline override block; the `over database …` data-context shortcut.
- **AUTHOR** (`CREATE BUILDING BLOCK`) so generated apps contribute blocks back to the
  Studio-Pro toolbox.
