// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"fmt"
	"strings"
)

// portCulpritAdvice renders the "how do I get out of this" half of a
// port-already-in-use error, naming the offending process when it can be
// resolved (see portowner_linux.go).
//
// Why this is worth the code: the guard's diagnosis used to be a guess ("a
// previous 'mxcli run --local' … is likely still serving on it") followed by a
// pgrep hunt. The guess is wrong as often as it is right — an orphan of a
// previous run and a colleague's unrelated server on 8080 need opposite
// remedies — and one of the suggested commands, `pgrep -f 'mxcli run'`, matches
// the shell it is typed into, so following the advice literally can kill your
// own session. Naming the pid turns three commands into one.
//
// Every line is indented two spaces to sit under the error's first line.
func portCulpritAdvice(port int, host string, appPort int) string {
	confirm := fmt.Sprintf(
		"    # confirm it is gone: curl -s -o /dev/null -w '%%{http_code}' http://%s:%d   (want 000)\n",
		host, appPort)

	owner, ok := listenerOnPort(port)
	if !ok {
		// Could not resolve — another user's process, or not Linux. Keep the
		// generic hunt, minus the pgrep pattern that matches the caller's own
		// shell.
		return "  Find and stop whatever is holding it, then retry:\n" +
			"    pgrep -af 'mxbuild|runtimelauncher'   # a previous run's orphans, if any\n" +
			"    kill <pid>\n" + confirm
	}

	var b strings.Builder
	fmt.Fprintf(&b, "  Held by pid %d", owner.PID)
	if owner.Cmdline != "" {
		fmt.Fprintf(&b, ": %s", owner.Cmdline)
	}
	b.WriteString("\n")

	if owner.Ours {
		// mxcli reaps its own children on Ctrl-C/SIGTERM, so reaching this state
		// means the previous run did not exit gracefully (kill -9, a crash, or a
		// reaped container). Saying so stops the user looking for a bug in the
		// guard.
		b.WriteString("  That is a leftover from an earlier run that did not shut down cleanly " +
			"(a kill -9 or a reaped container skips mxcli's own teardown).\n")
		fmt.Fprintf(&b, "    kill %d\n", owner.PID)
		b.WriteString(confirm)
		return b.String()
	}

	b.WriteString("  That is not a process mxcli started, so it is not a leftover run — " +
		"pick another port rather than killing it.\n")
	return b.String()
}
