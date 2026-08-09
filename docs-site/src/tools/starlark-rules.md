# Starlark Rules

In addition to the built-in Go rules, mxcli bundles 27 Starlark-based lint rules. Starlark is a Python-like language that allows rules to be extended and customized without recompiling mxcli.

## Bundled Starlark Rules

### Security Rules (SEC004-SEC009)

| Rule | Description |
|------|-------------|
| **SEC004** | Guest access -- Warns about overly permissive guest/anonymous access |
| **SEC005** | Strict mode -- Checks for strict security mode settings |
| **SEC006** | PII exposure -- Detects potentially sensitive data without restricted access |
| **SEC007** | Anonymous access -- Flags entities accessible to anonymous users |
| **SEC008** | Member restrictions -- Checks for overly broad member-level access |
| **SEC009** | Additional security rules |

### Architecture Rules (ARCH001-ARCH003)

| Rule | Description |
|------|-------------|
| **ARCH001** | Cross-module data -- Detects tight coupling between modules via direct data access |
| **ARCH002** | Microflow-based writes -- Ensures data modifications go through microflows |
| **ARCH003** | Entity business keys -- Checks that entities have meaningful business key attributes |

### Quality Rules (QUAL001-QUAL004)

| Rule | Description |
|------|-------------|
| **QUAL001** | McCabe complexity -- Flags microflows with high cyclomatic complexity |
| **QUAL002** | Documentation -- Missing documentation across every document type: modules, entities, pages, microflows, workflows, Java/JavaScript actions and their parameters, REST services, mappings, constants ([options](#qual002-options)) |
| **QUAL003** | Long microflows -- Warns about microflows with too many activities |
| **QUAL004** | Orphaned elements -- Detects unused entities, microflows, or pages |

### Design Rules (DESIGN001)

| Rule | Description |
|------|-------------|
| **DESIGN001** | Entity attribute count -- Warns when entities have too many attributes |

### Convention Rules (CONV001-CONV010, CONV015-CONV017)

| Rule | Description |
|------|-------------|
| **CONV001-CONV010** | Best practice conventions including boolean naming, page suffixes, enumeration prefixes, snippet prefixes |
| **CONV015** | Validation rules -- Checks for consistent validation patterns |
| **CONV016** | Event handlers -- Validates event handler configuration |
| **CONV017** | Calculated attributes -- Checks calculated attribute patterns |

Additional convention rules cover access rule constraints, role mapping, microflow size and content.

### QUAL002 options {#qual002-options}

Undocumented model elements are invisible to `mxcli check` and to the build —
nothing fails, so nothing reminds you. QUAL002 is that reminder, and it sweeps
**every document type a user authors**, not just the domain model.

**On by default** — one option per document type:

| Option | Element |
|---|---|
| `check_modules` | Module |
| `check_entities` | Entity |
| `check_pages` | Page |
| `check_snippets` | Snippet |
| `check_building_blocks` | Building block |
| `check_layouts` | Layout |
| `check_enumerations` | Enumeration |
| `check_microflows` | Microflow (nanoflows always exempt) |
| `check_java_actions` | Java action |
| `check_java_action_params` | Java action **parameter** |
| `check_javascript_actions` | JavaScript action |
| `check_workflows` | Workflow |
| `check_constants` | Constant |
| `check_image_collections` | Image collection |
| `check_data_transformers` | Data transformer |
| `check_business_event_services` | Business event service |
| `check_rest_clients` | Consumed REST client |
| `check_published_rest_services` | Published REST service |
| `check_json_structures` | JSON structure |
| `check_import_mappings` | Import mapping |
| `check_export_mappings` | Export mapping |

**Off by default** — members, suppressed for volume rather than for being
unimportant:

| Option | Element |
|---|---|
| `check_attributes` | Entity attribute |
| `check_associations` | Association |

Plus `min_activities` (default `3`): microflows with fewer activities are exempt,
on the grounds that a three-step flow's name says what a description would.

Why the split. A Java action has a handful of parameters, and Studio Pro renders
each description in the dialog where someone wires up the call — an undocumented
parameter is a blank field next to a name like `pInput` at exactly the moment a
caller has to decide what to pass. A domain model, by contrast, has hundreds of
attributes and associations, so the same check there is a wall of text rather
than a signal. Turn those two on when you are actively working through
documentation debt:

```yaml
rules:
  QUAL002:
    enabled: true
    options:
      check_attributes: true
      check_associations: true
      check_pages: false        # e.g. if page names are the convention here
      min_activities: 5
```

Elements in System and Marketplace modules are never reported — that is code you
did not write, and flagging Community Commons would bury the findings you can
act on. This applies to **every** rule, not just QUAL002: the `System` module is
not distinguishable by its `Source` (which is empty, exactly like your own
modules), so it used to slip past the Marketplace filter into every rule that
walks entities, pages, microflows or widgets. On a blank Mendix 9.24 project that
was the difference between 60 findings and 8.

Adding a document type to the sweep is two rows: one in `documentableSources`
(`mdl/linter/context.go`) naming the catalog table and its documentation column,
and one in `_DOC_KINDS` (`missing_documentation.star`) giving the option name and
the suggestion text. `TestQUAL002_SweepsEveryAdvertisedDocumentType` fails if the
Go side advertises a kind the tests do not cover.

## Where Starlark Rules Live

When you run `mxcli init`, Starlark rules are installed to:

```
your-project/
└── .claude/
    └── lint-rules/
        ├── sec004_guest_access.star
        ├── arch001_cross_module.star
        ├── qual001_complexity.star
        └── ...
```

## Running Starlark Rules

Starlark rules run automatically alongside built-in rules:

```bash
mxcli lint -p app.mpr
```

Use `--list-rules` to see all available rules:

```bash
mxcli lint -p app.mpr --list-rules
```
