// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mendixlabs/mxcli/cmd/mxcli/hubauth"
	"github.com/spf13/cobra"
)

// authHubCmd groups the tunnel-hub credential commands under `mxcli auth hub`.
// A hub API key authorizes `mxcli run --hub` to register a preview as you; get
// one from the hub's /cli browser page (or --token for headless use).
var authHubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Manage tunnel-hub API keys (mxcli run --hub)",
	Long: `Authenticate to a tunnel-hub so 'mxcli run --hub' can register previews
under your GitHub identity.

Get a key from the hub's browser page — open https://<hub>/cli, sign in with
GitHub, and copy the key. This works from any device (including Claude Code on the
web/mobile, whose container cannot reach GitHub's device endpoints). Set it as an
environment/repo secret so every session picks it up:

  MXCLI_HUB_KEY=<key>   # takes precedence over any stored key

'mxcli auth hub login --token <github-pat>' is the headless alternative (CI): it
mints a key from a GitHub token and caches it in ~/.mxcli/auth.json. 'status' and
'logout' inspect and revoke the stored key.

Open self-hosted hubs need no key — 'run --hub' falls back to the shared
--hub-secret.`,
}

var authHubLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Get a hub API key (browser page, or --token for headless use)",
	RunE:  runAuthHubLogin,
}

var authHubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether a hub API key is configured",
	RunE:  runAuthHubStatus,
}

var authHubLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Revoke and remove the stored hub API key",
	RunE:  runAuthHubLogout,
}

func init() {
	for _, c := range []*cobra.Command{authHubLoginCmd, authHubStatusCmd, authHubLogoutCmd} {
		c.Flags().String("hub", hubauth.DefaultHubURL, "hub base URL")
	}
	authHubLoginCmd.Flags().String("token", "", "GitHub token (PAT) to mint the hub key from, for headless use (CI); interactive users mint from the hub's /cli page instead")
	authHubCmd.AddCommand(authHubLoginCmd, authHubStatusCmd, authHubLogoutCmd)
	authCmd.AddCommand(authHubCmd)
}

func runAuthHubLogin(cmd *cobra.Command, _ []string) error {
	hubURL, _ := cmd.Flags().GetString("hub")
	token, _ := cmd.Flags().GetString("token")
	out := cmd.OutOrStdout()

	// The primary way to get a key is the hub's browser page — it works from any
	// device (including Claude Code web/mobile, whose container can't run GitHub's
	// device flow). This command covers the headless path: mint from a token.
	if token == "" {
		fmt.Fprintf(out, "To get a hub key, open this in a browser, sign in with GitHub, and copy the key:\n\n")
		fmt.Fprintf(out, "  %s/cli\n\n", strings.TrimRight(hubURL, "/"))
		fmt.Fprintf(out, "Then set it in your environment (a repo/environment secret in Claude Code):\n")
		fmt.Fprintf(out, "  export %s=<key>\n\n", hubauth.EnvHubKey)
		fmt.Fprintf(out, "Or, headless, mint from a GitHub token: mxcli auth hub login --token <github-pat>\n")
		return nil
	}

	client := &hubauth.Client{HubURL: hubURL}
	login, err := client.LoginWithToken(cmd.Context(), token)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "✓ Logged in as %s. Hub key saved for %s.\n", login, hubauth.HostOf(hubURL))
	fmt.Fprintf(out, "  'mxcli run --hub %s' will now register previews as %s.\n", hubURL, login)
	return nil
}

func runAuthHubStatus(cmd *cobra.Command, _ []string) error {
	hubURL, _ := cmd.Flags().GetString("hub")
	host := hubauth.HostOf(hubURL)
	out := cmd.OutOrStdout()

	if envKey := os.Getenv(hubauth.EnvHubKey); envKey != "" {
		fmt.Fprintf(out, "Hub:    %s\n", host)
		fmt.Fprintf(out, "Source: env (%s)\n", hubauth.EnvHubKey)
		fmt.Fprintln(out, "Status: a key is set via the environment (overrides any stored key).")
		return nil
	}
	if _, ok := hubauth.StoredKey(hubURL); ok {
		fmt.Fprintf(out, "Hub:    %s\n", host)
		fmt.Fprintln(out, "Source: file (~/.mxcli/auth.json)")
		fmt.Fprintln(out, "Status: a hub key is stored for this host.")
		return nil
	}
	fmt.Fprintf(out, "Hub:    %s\n", host)
	fmt.Fprintln(out, "Status: no hub key configured. Run: mxcli auth hub login")
	return nil
}

func runAuthHubLogout(cmd *cobra.Command, _ []string) error {
	hubURL, _ := cmd.Flags().GetString("hub")
	out := cmd.OutOrStdout()

	// Best-effort revoke on the hub before dropping the local copy.
	if key, ok := hubauth.StoredKey(hubURL); ok {
		client := &hubauth.Client{HubURL: hubURL}
		if err := client.RevokeHubKey(cmd.Context(), key); err != nil {
			fmt.Fprintf(out, "warning: could not revoke on the hub (%v); removing local copy anyway\n", err)
		}
	}
	if err := hubauth.DeleteKey(hubURL); err != nil {
		return fmt.Errorf("removing stored hub key: %w", err)
	}
	fmt.Fprintf(out, "Removed hub key for %s.\n", hubauth.HostOf(hubURL))
	return nil
}
