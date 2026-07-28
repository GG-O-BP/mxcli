// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/mendixlabs/mxcli/cmd/mxcli/hubauth"
	"github.com/spf13/cobra"
)

// authHubCmd groups the tunnel-hub credential commands under `mxcli auth hub`.
// A hub API key authorizes `mxcli run --hub` to register a preview as you; it is
// minted from your GitHub identity via the device flow and stored per hub host.
var authHubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Manage tunnel-hub API keys (mxcli run --hub)",
	Long: `Authenticate to a tunnel-hub so 'mxcli run --hub' can register previews
under your GitHub identity.

'mxcli auth hub login' runs the GitHub device flow, exchanges your identity for a
hub-minted API key, and caches it in ~/.mxcli/auth.json keyed by hub host. Your
GitHub token stays on this machine except for the single mint request to the hub.

For an unattended environment (e.g. Claude Code on the web, where the container is
reaped), set the key once as an environment/repo secret instead:

  MXCLI_HUB_KEY=<key>   # takes precedence over the stored key

Open self-hosted hubs need no key — 'run --hub' falls back to the shared
--hub-secret.`,
}

var authHubLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Mint and store a hub API key via GitHub device flow",
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
	authHubLoginCmd.Flags().String("token", "", "GitHub token (PAT) to mint the hub key from directly, skipping the device flow (use where the device flow is blocked, e.g. Claude Code web)")
	authHubCmd.AddCommand(authHubLoginCmd, authHubStatusCmd, authHubLogoutCmd)
	authCmd.AddCommand(authHubCmd)
}

func runAuthHubLogin(cmd *cobra.Command, _ []string) error {
	hubURL, _ := cmd.Flags().GetString("hub")
	token, _ := cmd.Flags().GetString("token")
	client := &hubauth.Client{HubURL: hubURL}

	var login string
	var err error
	if token != "" {
		// Direct token → key mint (no device flow). For environments whose egress
		// proxy blocks GitHub's device endpoints.
		login, err = client.LoginWithToken(cmd.Context(), token)
	} else {
		login, err = client.Login(cmd.Context(), cmd.OutOrStdout())
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n✓ Logged in as %s. Hub key saved for %s.\n", login, hubauth.HostOf(hubURL))
	fmt.Fprintf(cmd.OutOrStdout(), "  'mxcli run --hub %s' will now register previews as %s.\n", hubURL, login)
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
