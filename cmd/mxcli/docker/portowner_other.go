// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package docker

// portOwner describes the process listening on a local port. Only Linux can
// resolve it from /proc without shelling out, so elsewhere the port guard keeps
// its generic wording rather than depending on lsof being installed.
type portOwner struct {
	PID     int
	Cmdline string
	Ours    bool
}

// listenerOnPort always reports "unknown" off Linux.
func listenerOnPort(port int) (portOwner, bool) { return portOwner{}, false }
