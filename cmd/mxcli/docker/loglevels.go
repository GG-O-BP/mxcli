// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrAdminUnreachable marks a failure to reach the admin API at all, as opposed
// to the runtime rejecting the request. The two need different advice: "start an
// app" versus "that node does not exist", and printing both makes the real one
// harder to see.
var ErrAdminUnreachable = errors.New("admin API unreachable")

// loglevels.go drives the runtime's log-node levels through the M2EE admin API.
//
// Every fact below was established against a live 11.12.1 runtime, because the
// admin API is barely documented and guesses here are expensive:
//
//   - `get_log_settings` exists but REQUIRES one of `node`, `subscriber` or
//     `sort` in params. With none it answers "Please specify node, subscriber or
//     sort option in params" — an AdminException, not a usage hint, and the
//     detail is only in the runtime log rather than the HTTP response.
//   - `sort` accepts exactly "node" and "subscriber". Everything else
//     ("name", "all", "level", "nodes", …) is "Unknown sort option".
//   - `set_log_level` takes `{"nodes":[{"name":…,"level":…}], "force":bool}`.
//   - `force` means "allow a node that does not exist yet". Without it an
//     unknown node is REFUSED ("Unknown LogNode X. Use the 'force' parameter…"),
//     which makes the default the typo-safe one — so mxcli does not pass it
//     unless asked. Mendix supports pre-registering a level for a node that has
//     not published yet, which is what force is for.
//   - An invalid level is refused ("Unknown LogLevel VERBOSE").
//
// The response for an AdminException carries no detail ("See logging output for
// details"), so a failure here is reported with the request that caused it.
type LogLevel string

// The levels the runtime accepts, most to least severe. Verified against
// SetLoglevelAction's rejection of anything outside this set.
var logLevels = []string{"NONE", "CRITICAL", "ERROR", "WARNING", "INFO", "DEBUG", "TRACE"}

// NormalizeLogLevel upper-cases and validates a level, so a typo is caught here
// with the valid set rather than by an AdminException whose detail is in a log
// file the caller is not reading.
func NormalizeLogLevel(level string) (string, error) {
	up := strings.ToUpper(strings.TrimSpace(level))
	for _, l := range logLevels {
		if up == l {
			return up, nil
		}
	}
	return "", fmt.Errorf("unknown log level %q; valid levels are %s",
		level, strings.Join(logLevels, ", "))
}

// LogNodeLevel is one node and its level for a subscriber.
type LogNodeLevel struct {
	Node       string
	Subscriber string
	Level      string
}

// GetLogSettings returns every log node with its level, one row per
// node/subscriber pair, sorted by node then subscriber so output is stable.
func GetLogSettings(opts M2EEOptions) ([]LogNodeLevel, error) {
	resp, err := CallM2EE(opts, "get_log_settings", map[string]any{"sort": "node"})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAdminUnreachable, err)
	}
	if resp.Result != 0 {
		return nil, fmt.Errorf("reading log settings: %s", resp.M2EEError())
	}
	// feedback is {node: {subscriber: level}}.
	var out []LogNodeLevel
	for node, v := range resp.Feedback() {
		subs, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for sub, lvl := range subs {
			s, _ := lvl.(string)
			out = append(out, LogNodeLevel{Node: node, Subscriber: sub, Level: s})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !strings.EqualFold(out[i].Node, out[j].Node) {
			return strings.ToLower(out[i].Node) < strings.ToLower(out[j].Node)
		}
		return out[i].Subscriber < out[j].Subscriber
	})
	return out, nil
}

// SetLogLevels sets levels for the given nodes in a single admin call — the
// action takes a list, so a multi-node change is one round trip and lands
// together rather than partially.
//
// force allows a node the runtime does not know yet. Without it an unknown node
// is refused, which is the behaviour worth having by default: a mistyped node
// name should be an error, not a silently pre-registered setting that never
// takes effect.
func SetLogLevels(opts M2EEOptions, levels []LogNodeLevel, force bool) error {
	if len(levels) == 0 {
		return fmt.Errorf("no nodes given")
	}
	nodes := make([]map[string]any, 0, len(levels))
	var described []string
	for _, l := range levels {
		lvl, err := NormalizeLogLevel(l.Level)
		if err != nil {
			return err
		}
		nodes = append(nodes, map[string]any{"name": l.Node, "level": lvl})
		described = append(described, l.Node+"="+lvl)
	}
	params := map[string]any{"nodes": nodes}
	if force {
		params["force"] = true
	}

	resp, err := CallM2EE(opts, "set_log_level", params)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAdminUnreachable, err)
	}
	if resp.Result != 0 {
		// The HTTP response for an AdminException says only "See logging output
		// for details", so name the request and the likeliest cause instead of
		// passing that through as if it were an explanation.
		hint := ""
		if !force {
			hint = "\n  If the node does not exist yet, pass --force — the runtime refuses an unknown node otherwise."
		}
		return fmt.Errorf("setting log level (%s): %s%s",
			strings.Join(described, ", "), resp.M2EEError(), hint)
	}
	return nil
}

// MatchLogNodes returns the nodes whose name contains the (case-insensitive)
// pattern. An empty pattern matches everything.
func MatchLogNodes(all []LogNodeLevel, pattern string) []LogNodeLevel {
	if pattern == "" {
		return all
	}
	needle := strings.ToLower(pattern)
	var out []LogNodeLevel
	for _, l := range all {
		if strings.Contains(strings.ToLower(l.Node), needle) {
			out = append(out, l)
		}
	}
	return out
}
