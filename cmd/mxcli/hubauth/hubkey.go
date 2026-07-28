// SPDX-License-Identifier: Apache-2.0

package hubauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client talks to a hub's key API. The primary way to obtain a key is the hub's
// browser page (`https://<hub>/cli`) — sign in with GitHub, mint, copy into
// MXCLI_HUB_KEY. This client covers the headless path: mint directly from a
// GitHub token (`auth hub login --token`, CI) and revoke.
//
// There is deliberately no GitHub device flow: the target environment (Claude
// Code containers) blocks GitHub's device endpoints at the egress proxy, so the
// browser page is the real bootstrap.
type Client struct {
	HubURL string       // e.g. https://hub.mxcli.org
	HTTP   *http.Client // default http.DefaultClient
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) hub(path string) string {
	return strings.TrimRight(c.HubURL, "/") + path
}

// MintHubKey exchanges a GitHub token for a hub API key via POST /api/keys. The
// token is sent once, here, and never stored.
func (c *Client) MintHubKey(ctx context.Context, githubToken string) (key, login string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.hub("/api/keys"), nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+githubToken)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("minting hub key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", "", fmt.Errorf("hub key mint failed (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var body struct {
		Key   string `json:"key"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decoding hub key: %w", err)
	}
	return body.Key, body.Login, nil
}

// RevokeHubKey best-effort revokes a key on the hub (DELETE /api/keys).
func (c *Client) RevokeHubKey(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.hub("/api/keys"), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Hub-Key", key)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// LoginWithToken mints and stores a hub key from a GitHub token (PAT or OAuth
// token), for headless environments (CI, or a container that has a token but
// can't run a browser). Interactive users mint from the hub's /cli page instead.
func (c *Client) LoginWithToken(ctx context.Context, githubToken string) (login string, err error) {
	key, login, err := c.MintHubKey(ctx, strings.TrimSpace(githubToken))
	if err != nil {
		return "", err
	}
	if err := SaveKey(c.HubURL, key); err != nil {
		return "", fmt.Errorf("saving hub key: %w", err)
	}
	return login, nil
}
