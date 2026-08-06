// SPDX-License-Identifier: Apache-2.0

package theme

import "embed"

// assetsRoot is the embedded directory holding one folder per theme.
const assetsRoot = "assets"

// Themes are embedded so `mxcli new` works with no network and no companion
// files next to the binary. Each theme is assets/<name>/theme.json plus an
// assets/<name>/files/ tree that mirrors its layout inside the project.
//
// The `all:` prefix is load-bearing: a plain `go:embed assets` silently skips
// files whose name starts with "_", which is exactly how SCSS spells a partial.
// Without it _mxcli-signal.scss is missing from the binary and every generated
// app ships an @import pointing at nothing.
//
//go:embed all:assets
var assetsFS embed.FS
