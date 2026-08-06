---
title: Default styling — generated apps that look designed on first boot
status: partial
date: 2026-08-06
related:
  - PROPOSAL_atlas_design_system.md
  - docs/plans/2026-03-27-enhance-theme-system-design.md
  - .claude/skills/mendix/atlas-design.md
  - .claude/skills/mendix/theme-styling.md
---

# Default styling — generated apps that look designed on first boot

## Problem

`mxcli new` produces a blank Atlas app. It is unmistakably a blank Atlas app:
Mendix's stock blue gradient chrome, Open Sans, 8px radii, generous marketing-page
spacing. `PROPOSAL_atlas_design_system.md` already established the method for
fixing that — the 4-layer architecture, Atlas-first composition, the verify loop —
and shipped it as the `atlas-design` skill. What it did **not** ship is a default:
the Layer-1 scaffold in that skill is a template full of `// TODO: your brand
colour`, so every generated app either stays stock or gets a palette an agent
invented on the spot.

This proposal closes that gap. When the user does not ask for a theme, mxcli
applies a recognisable one.

## The design spec

The visual direction is **Signal** (from the design bundle, `1a` of three
concepts, with a fully worked reference app): cool slate ground, one teal signal
colour, 4px radius, 8px spacing unit, 32px grid rows and inputs, monospace for
every number/id/date, a 3px focus ring that is never suppressed, and a 44px
minimum touch target below the tablet breakpoint. `1b` Ledger and `1c` Console are
the alternates; both are a directory drop once the mechanism below exists.

## What the spec costs, split by where the value has to live

| Tier | What | Lives in | Phase |
|---|---|---|---|
| 1. Tokens + CSS | palette, IBM Plex, radius, density, focus ring, touch scaling, pills, KPI tiles | plain files under `theme/` — **no model changes at all** | **P1 (this proposal, shipped)** |
| 2. Model defaults | Plotly chart theme, `class:'num'` on numeric/date bindings, Atlas classes on generated CRUD pages | `.mpr` BSON — the page and chart builders | P2 |
| 3. Behaviour / structure | `/` search, J/K, ⌘S; phone bottom nav and sheets; tablet two-pane; dark mode | JS, new layouts, a full Atlas dark-override sheet | P3 |

Tier 1 is roughly 60% of the visual identity for a fraction of the work, and it is
pure file I/O — no BSON, no metamodel, no `mx check` exposure. That asymmetry
drives the phasing.

## Mechanics — settled empirically, not assumed

Everything below was verified against a real Mendix **11.13.0** project created
with `mx create-project`, compiled with `mxbuild --target=deploy`, and read back
out of `theme-cache/web/theme.compiled.css`. Two of the four findings contradict
what the existing skills say, which is why they are recorded here rather than
inferred.

1. **A theme source folder is only compiled when its name matches a real module.**
   A probe rule in `themesource/myfirstmodule/web/main.scss` compiled; the
   identical rule in `themesource/mxcli_theme/web/main.scss` did **not** — no
   error, no warning, silently absent from the output. So a theme must not invent
   a theme source folder. (`atlas-design.md` names
   `themesource/<mod>/web/main.scss` as the Layer-2 home; that is correct only
   when `<mod>` is a module the app actually has.)

2. **`theme/web/main.scss` compiles last** — after Atlas Core *and* after every
   module theme source (`.pill` landed at line 30805 of 30924; `.btn-primary` at
   19271). It is a three-line file of Mendix's own imports, not an Atlas-owned
   file, so a partial imported from it overrides anything without `!important`.
   This is the correct Layer-2 home for app-level styling.

3. **`theme/web/custom-variables.scss` is imported once per module** (8× in a
   blank app — `atlas_core/web/main.scss` pulls it in, and so does every module's
   own `main.scss`). It must therefore hold **declarations only**; a rule there is
   emitted once per module. This matches what Atlas itself does with the file.

4. **Mendix 11 Atlas is CSS-custom-property-first.** The stock
   `custom-variables.scss` is a `:root { --brand-primary: … }` block plus a few
   SCSS switches (`$font-family-import`, `$btn-bordered`, `$use-css-variables`),
   with legacy Sass variables mapped in `_css-variables-mappings.scss`. Layer 1 is
   therefore `:root` declarations, not SCSS `!default` variables. The derived ramp
   (`--brand-primary-50…900`) is built with CSS `color-mix()` against
   `var(--brand-primary)`, so it re-derives live from a retuned primary.

5. **`theme/web/<subdir>/` is deployed to the web root**, so vendored fonts at
   `theme/web/mxcli-fonts/` are served at `/mxcli-fonts/` and reachable from
   `theme.compiled.css` as `url("./mxcli-fonts/…")`.

## P1 — the drop (implemented)

### Shape

A theme is an embedded directory: a manifest plus a `files/` tree that mirrors its
layout inside the project.

```
cmd/mxcli/theme/
  theme.go              List / Get / Apply / Remove
  block.go              marker fencing + digest guard
  assets.go             //go:embed all:assets   <- `all:` is load-bearing
  assets/signal/
    theme.json
    files/theme/web/custom-variables.scss   Layer 1 token block
    files/theme/web/_mxcli-signal.scss      Layer 2 partial
    files/theme/web/main.scss               one @import line
    files/theme/web/mxcli-fonts/*.woff2     vendored IBM Plex (OFL 1.1)
```

`//go:embed assets` silently skips files whose name begins with `_` — which is
exactly how SCSS spells a partial. Without `all:` the binary ships an `@import`
pointing at nothing. A test asserts every `@font-face` URL resolves to a vendored
file for the same reason.

### Fencing — guard, don't drop

Two of the three text targets are files the project already owns, so a theme can
never rewrite a whole file. Each write is fenced:

```scss
// mxcli:theme:begin signal v1 — generated by `mxcli theme apply`; edit outside this block
…
// mxcli:theme:end signal b538a13336af87f6
```

The end marker carries a digest of the body. On re-apply the digest is recomputed
from disk: equal means mxcli's own output is still there and may be replaced,
different means a human edited inside the fence and the write is **refused**
unless `--force`. The record lives in the file itself — no sidecar state to drift.
This is ADR-0005's guard-don't-drop rule applied to files instead of BSON.

`theme remove` inverts it: blocks are cut back out, files that are wholly ours are
deleted, and directories the theme introduced are pruned — but only if empty, so a
user's own file in `mxcli-fonts/` survives.

### Surface

```
mxcli theme list                      # built-in themes, default marked
mxcli theme show signal               # tokens, colorway, files it writes
mxcli theme apply [name] -p app.mpr   # idempotent; --dry-run, --force
mxcli theme remove [name] -p app.mpr
mxcli new MyApp --version 11.13.0     # applies `signal` by default
mxcli new MyApp --version 11.13.0 --theme none
```

`--theme` is validated before MxBuild is downloaded, so a typo fails in a second
rather than after the slowest step in the command.

### Why fonts are vendored rather than `@import`ed from Google

The `@import url()` ordering trap (must be the first line or the browser drops it)
is a known gotcha in the catalog, but the stronger reasons are that a CDN font
breaks air-gapped and on-prem deployments, and adds a third-party request to every
page load of every generated app. IBM Plex is SIL OFL 1.1, so vendoring is clean:
7 latin woff2 files, ~144KB in the binary.

### Verification

`mx check` and a clean compile prove nothing about appearance, so this was taken
to a browser:

- `mxbuild --target=deploy` on a real 11.13.0 project — BUILD SUCCEEDED, tokens
  present in `theme.compiled.css`, 7 `@font-face` blocks, Layer-2 rules after all
  Atlas components.
- `mxcli run --local` + Playwright against the running app —
  `getComputedStyle(body).fontFamily` is `"IBM Plex Sans", …`,
  `--brand-primary` resolves to `#0f6e6b`, `--border-radius-s` to `4px`, and the
  woff2 is served `200 font/woff2`.
- A showcase page exercising the recipe classes, Atlas buttons, form inputs and a
  data grid, screenshotted in Chromium.

## P2 — model defaults (next)

The parts of the spec CSS cannot reach, in rough order of payoff:

- **Chart theme from the manifest colorway.** Chart series colour lives in the
  model (`customSeriesOptions`), not CSS — the one thing that does not re-skin
  when the palette changes. Injecting `customLayout` (transparent paper, themed
  ticks, faint grid), `customConfigurations` (`displayModeBar:false`) and per-type
  `customSeriesOptions` from `theme.json`'s `colorway` when a chart is created
  without explicit values closes the existing P2 ask in
  `PROPOSAL_atlas_design_system.md`.
- **`class:'num'` applied automatically** when a generator binds an
  Integer/Long/Decimal/DateTime/AutoNumber attribute. The generator knows the
  type; this is what turns "monospace for every number" from a class the author
  must remember into a property of generated output.
- **Atlas classes on generated CRUD pages** so the density and card shapes match
  the reference app.

## P3 — deferred

Keyboard-first interaction needs JS with no Mendix equivalent. Phone bottom-nav
and sheet navigation need layout documents. **Dark mode should stay deferred**:
the spec ships dark tokens under the same names, and that works for *our* classes,
but Atlas's own widgets and Plotly are light-only — the half-dark result is worse
than consistent light, as `PROPOSAL_atlas_design_system.md` established at cost.
When it lands it should be a committed single-theme variant (`signal-dark`) with
unconditional global widget overrides, not a `prefers-color-scheme` flip. Mendix
11 ships `:root.theme-dark` / `:root.theme-neutral` variant hooks in
`theme/web/`, which is the natural mechanism to build on.

## Open questions

- **Every mxcli app looking identical** is the point (shadcn and Vercel do exactly
  this), but it is a deliberate choice. Mitigated by putting `--brand-primary`
  first in the generated block with a one-line swap comment, rather than by
  randomising per app.
- **Ledger and Console** are near-free now — a `theme.json` plus two SCSS files
  each, and their fonts vendored. Worth shipping mainly to prove the registry is
  data-driven, and to give `--theme` something to choose between.
- **Should `mxcli init` apply the theme too?** Today only `new` does. `init` runs
  against existing projects that may already have styling, so applying there
  should probably stay explicit (`mxcli theme apply`).
