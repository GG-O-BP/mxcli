#!/bin/bash
#
# SessionStart hook — bootstrap a Claude Code on the web session for mxcli.
#
# The devcontainer (.devcontainer/Dockerfile) installs these prerequisites, but
# web sessions do not use the devcontainer, so a fresh container has no ANTLR4
# and `make build` fails at the `make grammar` step:
#
#     *** ANTLR4 not found. Install with: brew install antlr4 ... Stop.
#
# The generated parser in mdl/grammar/parser/ is deliberately not committed, so
# ANTLR4 is a hard build dependency, not an optional extra.
#
# Idempotent and non-interactive: safe to re-run on resume/clear/compact.
set -euo pipefail

# Pinned to match .github/workflows/push-test.yml. The antlr4 wrapper resolves
# the jar version from this variable, so it must be set for every build, not
# just this script — hence the CLAUDE_ENV_FILE export below.
ANTLR_VERSION='4.13.2'
ANTLR_TOOLS_VERSION='0.2.2'

# Local (devcontainer / laptop) setups already have these via the Dockerfile.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-$(dirname "$0")/../..}"

# 1. ANTLR4 — required by `make grammar`, which `make build` always runs.
if ! command -v antlr4 >/dev/null 2>&1; then
  echo "Installing antlr4-tools==${ANTLR_TOOLS_VERSION}..."
  pip install --break-system-packages --quiet "antlr4-tools==${ANTLR_TOOLS_VERSION}"
fi

# The antlr4 wrapper downloads its jar on first use. Doing it here keeps that
# ~2MB fetch (and its JDK probe) out of the first `make build`.
export ANTLR4_TOOLS_ANTLR_VERSION="${ANTLR_VERSION}"
if [ ! -d "${HOME}/.m2/repository/org/antlr/antlr4/${ANTLR_VERSION}" ]; then
  echo "Fetching ANTLR ${ANTLR_VERSION} jar..."
  antlr4 >/dev/null 2>&1 || true
fi

# Persist for the session so `make build` works from any later shell.
if [ -n "${CLAUDE_ENV_FILE:-}" ]; then
  echo "export ANTLR4_TOOLS_ANTLR_VERSION=${ANTLR_VERSION}" >> "${CLAUDE_ENV_FILE}"
fi

# 2. Go modules — warms the module cache so the first build is not a ~1GB fetch.
echo "Downloading Go modules..."
go mod download

# 3. MxBuild — the Mendix toolchain that validates projects mxcli writes
#    (`mx check`). Opt-in: the CDN tarball is ~820MB and unpacks to ~1.6GB, too
#    slow to pull into every session start. Set the version to enable, e.g.
#    MXCLI_HOOK_MXBUILD_VERSION=11.13.0 (the newest in the nightly CI matrix).
#    Otherwise fetch on demand: mxcli setup mxbuild --version 11.13.0
if [ -n "${MXCLI_HOOK_MXBUILD_VERSION:-}" ]; then
  if [ ! -d "${HOME}/.mxcli/mxbuild/${MXCLI_HOOK_MXBUILD_VERSION}" ]; then
    echo "Downloading MxBuild ${MXCLI_HOOK_MXBUILD_VERSION} (~820MB)..."
    go run ./cmd/mxcli setup mxbuild --version "${MXCLI_HOOK_MXBUILD_VERSION}"
  fi
fi

echo "Session bootstrap complete. Build with: make build"
