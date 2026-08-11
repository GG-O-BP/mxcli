---
description: Generate a prefilled compare URL to open a PR into mendixlabs/mxcli
argument-hint: [--head <branch>]
---

# /mxcli-dev:upstream-pr — Link to open a PR into upstream (mendixlabs/mxcli)

Generate a prefilled GitHub **compare** URL that opens a PR merging this fork
(`ako/mxcli`) into the upstream fork (`mendixlabs/mxcli`).

**Why a link instead of opening the PR directly:** `mendixlabs/mxcli` is not in
this session's tooling scope, so the PR can't be created via the GitHub API. The
compare URL prefills the title and body; the user opens it and clicks "Create
pull request".

## Steps

1. Confirm what's actually unmerged upstream. If you have (or can fetch) the
   upstream base, build the range explicitly:
   ```bash
   git fetch https://github.com/mendixlabs/mxcli main
   git log --no-merges --oneline FETCH_HEAD..HEAD
   ```
   If the fetch is blocked or unnecessary, fall back to summarising the fork's
   `main` since the last sync.
2. Draft a concise **title** and a Markdown **body** grouping the changes by
   theme (one bullet per finding/fix). Reuse the structure from the last sync PR.
3. Generate the link with the script — pass the body on stdin so multi-line
   Markdown encodes cleanly:
   ```bash
   scripts/upstream-pr-link.sh --title "<title>" --body-file - <<'BODY'
   <markdown body>
   BODY
   ```
   Or let it auto-build the body from commits:
   ```bash
   scripts/upstream-pr-link.sh --commits FETCH_HEAD..HEAD
   ```
4. Present to the user:
   - the prefilled compare URL,
   - the **title** and **body** as plain text (fallback if the browser trims a
     long prefilled body).
5. Remind the user that `mendixlabs/mxcli` isn't in scope, so this is a link —
   offer to `add_repo` and open the PR via API if they'd rather.

## Notes

- Defaults are `ako/mxcli:main → mendixlabs/mxcli:main`. Override with
  `--fork`, `--upstream`, `--base`, `--head` for other syncs.
- Do **not** include the model identifier in the title or body.
- This is a link generator only — it does not push, commit, or open anything.
