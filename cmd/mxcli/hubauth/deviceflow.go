// SPDX-License-Identifier: Apache-2.0

package hubauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client carries the transport + endpoints for a login. Zero value is production
// (real GitHub, http.DefaultClient); tests override the bases + clock.
type Client struct {
	HubURL     string       // e.g. https://hub.mxcli.org
	HTTP       *http.Client // default http.DefaultClient
	GitHubBase string       // default https://github.com (device code + token poll)

	// now/sleep are injectable so tests don't wall-clock through the poll loop.
	now   func() time.Time
	sleep func(context.Context, time.Duration)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) githubBase() string {
	if c.GitHubBase != "" {
		return strings.TrimRight(c.GitHubBase, "/")
	}
	return "https://github.com"
}

func (c *Client) sleepFor(ctx context.Context, d time.Duration) {
	if c.sleep != nil {
		c.sleep(ctx, d)
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// HubAuthConfig is the client-visible slice of GET /api/auth-config.
type HubAuthConfig struct {
	AuthEnabled    bool   `json:"authEnabled"`
	RequireAuth    bool   `json:"requireAuth"`
	GitHubClientID string `json:"githubClientId"`
}

// FetchAuthConfig asks the hub whether GitHub auth is required and, if so, which
// OAuth App client id to use for the device flow.
func (c *Client) FetchAuthConfig(ctx context.Context) (HubAuthConfig, error) {
	var cfg HubAuthConfig
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.hub("/api/auth-config"), nil)
	if err != nil {
		return cfg, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return cfg, fmt.Errorf("contacting hub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cfg, fmt.Errorf("hub auth-config returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("decoding hub auth-config: %w", err)
	}
	return cfg, nil
}

func (c *Client) hub(path string) string {
	return strings.TrimRight(c.HubURL, "/") + path
}

// DeviceCode is GitHub's device-flow authorization prompt.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode begins the GitHub device flow for the given OAuth client id.
func (c *Client) RequestDeviceCode(ctx context.Context, clientID string) (DeviceCode, error) {
	var dc DeviceCode
	form := url.Values{"client_id": {clientID}, "scope": {"read:user"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.githubBase()+"/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return dc, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return dc, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dc, fmt.Errorf("device-code request returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&dc); err != nil {
		return dc, fmt.Errorf("decoding device code: %w", err)
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return dc, nil
}

// tokenPollResponse is GitHub's device-token endpoint reply (success or a
// pending/slow-down/error signal in `error`).
type tokenPollResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

// PollForToken polls GitHub until the user authorizes, honouring the
// authorization_pending / slow_down backoff. Returns the GitHub access token.
func (c *Client) PollForToken(ctx context.Context, clientID string, dc DeviceCode) (string, error) {
	interval := time.Duration(dc.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(max(dc.ExpiresIn, 300)) * time.Second)
	if c.now != nil {
		deadline = c.now().Add(time.Duration(max(dc.ExpiresIn, 300)) * time.Second)
	}
	for {
		c.sleepFor(ctx, interval)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		tok, e, err := c.pollOnce(ctx, clientID, dc.DeviceCode)
		if err != nil {
			return "", err
		}
		switch e {
		case "":
			if tok != "" {
				return tok, nil
			}
		case "authorization_pending":
			// keep polling at the current interval
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return "", fmt.Errorf("the device code expired before authorization; run 'mxcli auth hub login' again")
		case "access_denied":
			return "", fmt.Errorf("authorization was denied")
		default:
			return "", fmt.Errorf("device authorization failed: %s", e)
		}
		nowT := time.Now()
		if c.now != nil {
			nowT = c.now()
		}
		if nowT.After(deadline) {
			return "", fmt.Errorf("timed out waiting for device authorization")
		}
	}
}

func (c *Client) pollOnce(ctx context.Context, clientID, deviceCode string) (token, errCode string, err error) {
	form := url.Values{
		"client_id":   {clientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.githubBase()+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("polling for token: %w", err)
	}
	defer resp.Body.Close()
	var body tokenPollResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return "", "", fmt.Errorf("decoding token poll: %w", err)
	}
	return body.AccessToken, body.Error, nil
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

// Login runs the full bootstrap: discover the client id, device flow, mint a hub
// key, and store it. Prompts are written to out. Returns the resolved GitHub
// login. The GitHub token never leaves this process except for the mint call.
func (c *Client) Login(ctx context.Context, out io.Writer) (login string, err error) {
	cfg, err := c.FetchAuthConfig(ctx)
	if err != nil {
		return "", err
	}
	if !cfg.AuthEnabled {
		return "", fmt.Errorf("hub %s does not require authentication (open mode); no key needed", HostOf(c.HubURL))
	}
	if cfg.GitHubClientID == "" {
		return "", fmt.Errorf("hub reports auth enabled but advertises no GitHub client id")
	}
	dc, err := c.RequestDeviceCode(ctx, cfg.GitHubClientID)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, "\nTo authorize this device, open:\n  %s\nand enter the code:\n  %s\n\nWaiting for authorization...\n",
		dc.VerificationURI, dc.UserCode)

	token, err := c.PollForToken(ctx, cfg.GitHubClientID, dc)
	if err != nil {
		return "", err
	}
	key, login, err := c.MintHubKey(ctx, token)
	if err != nil {
		return "", err
	}
	if err := SaveKey(c.HubURL, key); err != nil {
		return "", fmt.Errorf("saving hub key: %w", err)
	}
	return login, nil
}
