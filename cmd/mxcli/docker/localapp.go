// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mendixlabs/mxcli/sdk/mpr"
)

// LocalAppOptions configures StartLocalApp.
type LocalAppOptions struct {
	// ProjectPath is the .mpr file.
	ProjectPath string
	// DeployDir is the deployment directory (default <project dir>/deployment).
	DeployDir string
	// AppPort / AdminPort / ServePort default to 8080 / 8090 / 6543.
	AppPort   int
	AdminPort int
	ServePort int
	// AdminPass is the M2EE admin password (defaults to the local-run password).
	AdminPass string
	// DB is the database to connect to; empty fields take the run --local
	// defaults (PostgreSQL at 127.0.0.1:5432, user/password mendix, database
	// name derived from the project file name).
	DB DBConfig
	// EnsureDB provisions the local Postgres + database when missing instead of
	// only checking reachability.
	EnsureDB bool
	// SkipBuild boots against whatever is already in DeployDir.
	SkipBuild bool
	// RuntimeLogPath tees the runtime JVM output and the runtime's own
	// application log to this file.
	RuntimeLogPath string
	// Env are extra "KEY=value" entries for the runtime JVM (see
	// LocalRuntimeOptions.Env) — how a secret reaches the runtime without being
	// written to disk.
	Env []string
	// Stdout/Stderr receive progress messages.
	Stdout io.Writer
	Stderr io.Writer
}

// LocalApp is a booted local app: an mxbuild serve server plus the standalone
// runtime it deployed to. It is the Docker-free equivalent of `docker compose
// up` for callers that need an app running and then stopped again.
type LocalApp struct {
	Runtime *LocalRuntime
	// Version is the project's Mendix version.
	Version string
	// RuntimeLogPath is where the runtime log is being written (may be empty).
	RuntimeLogPath string

	serve *ServeServer
}

func (o *LocalAppOptions) applyDefaults() {
	// Resolve the project path before anything is derived from it: DeployDir
	// below, and the runtime's own working directory, both hang off it, and a
	// relative value would leave them relative to whatever cwd the caller
	// happened to have. ServeServer.Build absolutizes too — that is the backstop
	// for MxBuild's own requirement; this is so the paths around it agree.
	if o.ProjectPath != "" && !filepath.IsAbs(o.ProjectPath) {
		if abs, err := filepath.Abs(o.ProjectPath); err == nil {
			o.ProjectPath = abs
		}
	}
	if o.DeployDir == "" {
		o.DeployDir = filepath.Join(filepath.Dir(o.ProjectPath), "deployment")
	}
	if o.AppPort == 0 {
		o.AppPort = 8080
	}
	if o.AdminPort == 0 {
		o.AdminPort = 8090
	}
	if o.ServePort == 0 {
		o.ServePort = 6543
	}
	if o.AdminPass == "" {
		o.AdminPass = defaultLocalAdminPass
	}
	if o.DB.Type == "" {
		o.DB.Type = "PostgreSQL"
	}
	if o.DB.Host == "" {
		o.DB.Host = "127.0.0.1:5432"
	}
	if o.DB.User == "" {
		o.DB.User = "mendix"
	}
	if o.DB.Password == "" {
		o.DB.Password = "mendix"
	}
	if o.DB.Name == "" {
		o.DB.Name = deriveDBName(o.ProjectPath)
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
}

// StartLocalApp builds the project with mxbuild and boots the standalone
// runtime against the result — the same sequence as `mxcli run --local`, minus
// everything a headless caller does not need (no web client bundle, no hub, no
// watch loop, no screenshots).
//
// The caller owns the returned app and must Stop it.
func StartLocalApp(opts LocalAppOptions) (*LocalApp, error) {
	opts.applyDefaults()
	w := opts.Stdout

	if err := checkLocalAppPortsFree(opts); err != nil {
		return nil, err
	}

	// 1. Project version → which mxbuild and runtime to use.
	reader, err := mpr.Open(opts.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("opening project: %w", err)
	}
	version := reader.ProjectVersion().ProductVersion
	reader.Close()

	// 2. Cache mxbuild + runtime (no-ops when already present).
	if _, err := DownloadMxBuild(version, w); err != nil {
		return nil, fmt.Errorf("setting up mxbuild: %w", err)
	}
	installPath, err := resolveRuntimeInstall(version, w)
	if err != nil {
		return nil, fmt.Errorf("setting up runtime: %w", err)
	}

	// 3. Database.
	if opts.EnsureDB {
		if err := EnsureDatabase(opts.DB, w); err != nil {
			return nil, fmt.Errorf("ensuring database: %w", err)
		}
	} else if err := pingTCP(opts.DB.Host, 3*time.Second); err != nil {
		return nil, fmt.Errorf("database not reachable at %s: %w\n"+
			"  Pass --ensure-db to provision it, or start Postgres and create the %q database (user %q).",
			opts.DB.Host, err, opts.DB.Name, opts.DB.User)
	}

	app := &LocalApp{Version: version, RuntimeLogPath: opts.RuntimeLogPath}

	// 4. Build, unless the caller is reusing an existing deployment.
	if !opts.SkipBuild {
		fmt.Fprintln(w, "Building project (mxbuild --serve)...")
		serve, err := StartServe(ServeOptions{Version: version, Host: "127.0.0.1", Port: opts.ServePort})
		if err != nil {
			return nil, fmt.Errorf("starting mxbuild serve: %w", err)
		}
		app.serve = serve

		build, err := serve.Build(BuildRequest{Target: TargetDeploy, ProjectFilePath: opts.ProjectPath})
		if err != nil {
			app.Stop()
			return nil, fmt.Errorf("build: %w", err)
		}
		if !build.OK() {
			app.Stop()
			return nil, fmt.Errorf("build failed: %s\n%s", build.Message, string(build.Raw))
		}
	}

	// 5. Boot the runtime against the deployment.
	rt, err := StartLocalRuntime(LocalRuntimeOptions{
		DeployDir:      opts.DeployDir,
		InstallPath:    installPath,
		AppPort:        opts.AppPort,
		AdminPort:      opts.AdminPort,
		AdminPass:      opts.AdminPass,
		DB:             opts.DB,
		RuntimeLogPath: opts.RuntimeLogPath,
		Env:            opts.Env,
		Stdout:         opts.Stdout,
		Stderr:         opts.Stderr,
	})
	if err != nil {
		app.Stop()
		return nil, err
	}
	app.Runtime = rt
	return app, nil
}

// Stop shuts down the runtime and the build server. Safe to call more than once
// and on a partially-started app.
func (a *LocalApp) Stop() error {
	var firstErr error
	if a.Runtime != nil {
		if err := a.Runtime.Stop(); err != nil {
			firstErr = err
		}
		a.Runtime = nil
	}
	if a.serve != nil {
		if err := a.serve.Stop(); err != nil && firstErr == nil {
			firstErr = err
		}
		a.serve = nil
	}
	return firstErr
}

// checkLocalAppPortsFree refuses to boot onto a port something is already
// serving — otherwise a stale runtime is silently adopted and the caller reads
// results from an app it did not build.
func checkLocalAppPortsFree(o LocalAppOptions) error {
	ports := []struct {
		port int
		what string
	}{
		{o.AppPort, "app"},
		{o.AdminPort, "runtime admin API"},
	}
	if !o.SkipBuild {
		ports = append(ports, struct {
			port int
			what string
		}{o.ServePort, "mxbuild serve"})
	}
	for _, p := range ports {
		if err := pingTCP(fmt.Sprintf("127.0.0.1:%d", p.port), 300*time.Millisecond); err == nil {
			return fmt.Errorf("port %d (%s) is already in use — stop the running instance first, "+
				"or pass a different port", p.port, p.what)
		}
	}
	return nil
}
