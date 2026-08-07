// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// javadeps.go resolves a project's managed Java (JAR) dependencies.
//
// mxcli-formula1 findings #12: `ALTER MODULE X ADD JAR DEPENDENCY (…)` writes the
// coordinate to the model and `list jar dependencies` reports it — but the jar
// never reaches the classpath, and the only symptom is a runtime SQLException
// about a missing driver, long after the model looked right.
//
// The cause is not a bad write. Measured on 11.12.1: a full `mxbuild
// --target=deploy` produces a `deployment/build.gradle` with no dependencies
// block and downloads nothing, while `mx sync-java-dependencies <project.mpr>`
// fetches the jar into `vendorlib/`. Resolution is a separate step that Studio
// Pro runs for you when you edit Module Settings, and that nothing was running
// headless.

// SyncJavaDependencies runs `mx sync-java-dependencies` against projectPath,
// which downloads every declared managed dependency into the project's
// vendorlib/. It needs network access (Maven) and the mx binary.
//
// mxPath may be empty, in which case mx is resolved from the version's cache.
func SyncJavaDependencies(projectPath, mxPath, version string, w io.Writer) error {
	if mxPath == "" {
		mxPath = CachedMxPath(version)
	}
	if mxPath == "" {
		// No mx for this exact version. Fall back to any available one — a
		// newer mx reads an older project fine — but say so, because a
		// version mismatch is otherwise invisible until mx complains about
		// the mpr format and the message reads as a corrupt project.
		if resolved, err := ResolveMxForVersion("", version); err == nil && resolved != "" {
			mxPath = resolved
			if w != nil {
				fmt.Fprintf(w, "  Note: no mx for Mendix %s; using %s. Run 'mxcli setup mxbuild --version %s' if this misbehaves.\n",
					version, mxPath, version)
			}
		}
	}
	if mxPath == "" {
		return fmt.Errorf("mx not found for Mendix %s (needed to resolve managed Java dependencies); run 'mxcli setup mxbuild --version %s'", version, version)
	}

	cmd := exec.Command(mxPath, "sync-java-dependencies", projectPath)
	cmd.Dir = filepath.Dir(projectPath)
	PrepareMxCommand(cmd) // FreeType LD_PRELOAD workaround

	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mx sync-java-dependencies failed: %w\n%s", err, lastLines(out.String(), 20))
	}
	if w != nil {
		if s := strings.TrimSpace(out.String()); s != "" {
			fmt.Fprintln(w, "  "+strings.ReplaceAll(s, "\n", "\n  "))
		}
	}
	return nil
}

// UnvendoredJarDependencies returns the coordinates ("group:artifact:version")
// declared by the model that have no matching jar in the project's vendorlib/,
// so a caller can sync only when something is actually missing.
//
// The vendored file name is `<artifact>-<version>.jar`, which is what
// `mx sync-java-dependencies` writes.
func UnvendoredJarDependencies(projectDir string, deps []JarDependencyRef) []string {
	var missing []string
	for _, d := range deps {
		if d.Artifact == "" || d.Version == "" {
			continue
		}
		jar := filepath.Join(projectDir, "vendorlib", fmt.Sprintf("%s-%s.jar", d.Artifact, d.Version))
		if _, err := os.Stat(jar); err != nil {
			missing = append(missing, fmt.Sprintf("%s:%s:%s", d.Group, d.Artifact, d.Version))
		}
	}
	return missing
}

// JarDependencyRef is the coordinate of a managed Java dependency, decoupled
// from the model types so this package does not depend on the executor.
type JarDependencyRef struct {
	Group    string
	Artifact string
	Version  string
}
