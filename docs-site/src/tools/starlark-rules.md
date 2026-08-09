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
| **QUAL002** | Documentation -- Missing documentation on entities, microflows, Java actions and Java action parameters ([options](#qual002-options)) |
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
nothing fails, so nothing reminds you. QUAL002 is that reminder, and it covers
more than the domain model:

| Option | Default | Checks |
|---|---|---|
| `check_entities` | `true` | entity descriptions |
| `check_microflows` | `true` | microflow descriptions (nanoflows always exempt) |
| `check_java_actions` | `true` | Java action documentation |
| `check_java_action_params` | `true` | Java action **parameter** descriptions |
| `check_attributes` | `false` | entity attribute descriptions |
| `min_activities` | `3` | microflows with fewer activities are exempt |

Parameter descriptions are worth more than they look: Studio Pro renders them in
the dialog where someone wires up the call, so an undocumented parameter is a
blank field next to a name like `pInput` at exactly the moment a caller has to
decide what to pass. Java actions matter for the same reason one step up — unlike
a microflow, the body is Java the model cannot show a reader.

`check_attributes` is off by default only because of volume: a real domain model
has hundreds of attributes, and turning it on at `info` severity is a long list.
Turn it on when you are actually working through documentation debt.

Set options in the lint config:

```yaml
rules:
  QUAL002:
    enabled: true
    options:
      check_attributes: true
      min_activities: 5
```

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
