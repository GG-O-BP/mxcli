// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"
)

// stdinPath is the conventional spelling for "read from standard input".
const stdinPath = "-"

// readMDLSource reads an MDL script from a file, or from standard input when the
// path is "-".
//
// A heredoc is the natural way to drive MDL from an agent or a shell script, and
// `-` is how every other Unix tool spells it — without this the dash was taken
// literally and the command failed with "open -: no such file or directory",
// forcing a temp file. (mxcli-todo findings #5)
func readMDLSource(path string) ([]byte, error) {
	if path == stdinPath {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading MDL from stdin: %w", err)
		}
		return content, nil
	}
	return os.ReadFile(path)
}

// mdlSourceLabel names the source in messages: a real path, or "<stdin>".
func mdlSourceLabel(path string) string {
	if path == stdinPath {
		return "<stdin>"
	}
	return path
}
