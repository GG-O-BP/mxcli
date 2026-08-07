// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// screenshot_login.go logs into a secured Mendix app once and saves a Playwright
// storage state (cookies + local storage), so subsequent screenshots of pages
// behind login use `playwright screenshot --load-storage`. The `playwright` CLI
// has no scriptable login, so this drives a tiny headless script via the same
// Playwright install (resolved from the CLI's package dir — no hardcoded paths).
//
// The Mendix Atlas login page exposes stable selectors: #usernameInput,
// #passwordInput, #loginButton. If no login form appears (anonymous/public app),
// the script still saves storage and proceeds.

// resolvePlaywrightPkgDir returns the directory of the installed `playwright`
// package (the parent for requiring playwright-core), or "" if not found.
func resolvePlaywrightPkgDir() string {
	bin, err := exec.LookPath("playwright")
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		real = bin
	}
	// real is <pkg>/cli.js; the package dir is its directory.
	dir := filepath.Dir(real)
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return ""
	}
	return dir
}

// resolveNodeForScript returns a node binary to run the login script: the system
// node if on PATH, else mxbuild's bundled node resolved from mxbuildPath.
func resolveNodeForScript(mxbuildPath string) string {
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	if mxbuildPath != "" {
		if nodeBin, _, err := resolveNodeTooling(mxbuildPath); err == nil {
			return nodeBin
		}
	}
	return ""
}

// loginScript is the headless login+save-storage script. It requires
// playwright-core from the resolved package dir (arg 1). Remaining args:
// appURL, username, password, storagePath.
const loginScript = `
const pkgDir = process.argv[2];
const [appURL, username, password, storagePath] = process.argv.slice(3);
const { chromium } = require(require.resolve("playwright-core", { paths: [pkgDir] }));
(async () => {
  const b = await chromium.launch();
  const ctx = await b.newContext();
  const p = await ctx.newPage();
  await p.goto(appURL, { waitUntil: "load", timeout: 30000 });
  let sawForm = false;
  let failure = "";
  try {
    await p.waitForSelector("#usernameInput", { timeout: 8000 });
    sawForm = true;
    await p.fill("#usernameInput", username);
    await p.fill("#passwordInput", password);
    await Promise.all([
      p.waitForNavigation({ waitUntil: "load", timeout: 20000 }).catch(() => {}),
      p.click("#loginButton"),
    ]);
    await p.waitForTimeout(2500);
  } catch (e) {
    // No login form within the timeout: anonymous or already authenticated.
    process.stderr.write("login: no login form detected (" + e.message.split("\n")[0] + ")\n");
  }
  // Mendix answers a rejected sign-in by re-rendering the same form, so the
  // username field still being there is the signal that login did not complete.
  // Without this the script saved an anonymous session and every later
  // screenshot silently showed the login page instead of the requested page.
  if (sawForm && (await p.locator("#usernameInput").count()) > 0) {
    const alert = ((await p.locator(".alert").first().textContent().catch(() => "")) || "")
      .replace(/\s+/g, " ").trim().slice(0, 200);
    failure = alert || "still on the login page after submitting";
  }
  await ctx.storageState({ path: storagePath });
  await b.close();
  if (failure) {
    process.stderr.write("login: sign-in did not complete: " + failure + "\n");
    process.exit(2);
  }
})().catch((e) => { process.stderr.write(String(e) + "\n"); process.exit(1); });
`

// LoginOptions configures LoginAndSaveStorage.
type LoginOptions struct {
	AppURL      string
	Username    string
	Password    string
	StoragePath string // where to write the Playwright storage state JSON
	MxBuildPath string // fallback node source
	// RuntimeLogPath is consulted when a sign-in is rejected: the runtime
	// records why, and the page does not.
	RuntimeLogPath string
	Timeout        time.Duration // default 60s
}

// sessionCapMarker is what the unlicensed runtime logs when it refuses a sign-in
// because every session slot is taken. The browser only ever says "Sign in
// failed", which points at the credentials — so the cause is invisible unless
// someone thinks to open the runtime log.
const sessionCapMarker = "Maximum number of sessions exceeded"

// loginFailureHint returns an explanation to append to a failed sign-in, read
// from the tail of the runtime log. Empty when the log says nothing useful.
func loginFailureHint(runtimeLogPath string) string {
	if runtimeLogPath == "" || runtimeLogPath == "-" {
		return ""
	}
	tail, err := readLogTail(runtimeLogPath, 64*1024)
	if err != nil || !strings.Contains(tail, sessionCapMarker) {
		return ""
	}
	return "\nThe runtime log reports: " + sessionCapMarker + " — the unlicensed local\n" +
		"runtime allows only a handful of concurrent sessions, and the login page reports\n" +
		"that as \"Sign in failed\" as if the password were wrong. Sessions are released by\n" +
		"restarting `mxcli run --local`; a script that drives the app through a browser\n" +
		"should sign out when it finishes so it does not consume a slot per run."
}

// readLogTail reads at most the last n bytes of a file.
func readLogTail(path string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if off := info.Size() - n; off > 0 {
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return "", err
		}
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LoginAndSaveStorage logs into the app and writes a Playwright storage-state
// file to opts.StoragePath. That file is then passed to CaptureScreenshot via
// StoragePath (screenshot --load-storage) to shoot pages behind login.
func LoginAndSaveStorage(opts LoginOptions) error {
	if opts.StoragePath == "" {
		return fmt.Errorf("storage path is required")
	}
	pkgDir := resolvePlaywrightPkgDir()
	if pkgDir == "" {
		return fmt.Errorf("playwright package not found (install with: npm i -g playwright)")
	}
	node := resolveNodeForScript(opts.MxBuildPath)
	if node == "" {
		return fmt.Errorf("node not found to run the login script")
	}
	if err := os.MkdirAll(filepath.Dir(opts.StoragePath), 0o755); err != nil {
		return err
	}

	// .cjs so Node treats it as CommonJS (the script uses require()).
	scriptPath := filepath.Join(filepath.Dir(opts.StoragePath), ".mxcli-login.cjs")
	if err := os.WriteFile(scriptPath, []byte(loginScript), 0o644); err != nil {
		return fmt.Errorf("writing login script: %w", err)
	}
	defer os.Remove(scriptPath)

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	cmd := exec.Command(node, scriptPath, pkgDir, opts.AppURL, opts.Username, opts.Password, opts.StoragePath)
	cmd.Env = os.Environ() // PLAYWRIGHT_BROWSERS_PATH resolves Chromium
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launching login script: %w", err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("login failed: %w\n%s%s", err, out.String(), loginFailureHint(opts.RuntimeLogPath))
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("login timed out after %s", timeout)
	}
	if _, err := os.Stat(opts.StoragePath); err != nil {
		return fmt.Errorf("login reported success but %s is missing:\n%s", opts.StoragePath, out.String())
	}
	return nil
}
