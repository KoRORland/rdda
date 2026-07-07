package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/KoRORland/rdda/internal/cfconfig"
	"github.com/KoRORland/rdda/internal/cfsetup"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

const cloudflaredDir = "/etc/cloudflared"

// cfEnv is the side-effect surface of `rdda cf setup`, injected so the
// orchestration (create-or-reuse tunnel, write config, route + verify DNS) can
// be tested without a live Cloudflare account or mutating the host.
type cfEnv struct {
	dir            string
	out            io.Writer
	dryRun         bool
	run            func(name string, args ...string) (string, error) // capture combined output
	runAttached    func(name string, args ...string) error           // stream to terminal (login)
	exists         func(path string) bool
	writeFile      func(path string, data []byte, perm os.FileMode) error
	mkdirAll       func(path string, perm os.FileMode) error
	hasCloudflared func() bool
}

func realCFEnv(dir string, out io.Writer, dryRun bool) cfEnv {
	return cfEnv{
		dir: dir, out: out, dryRun: dryRun,
		run: func(name string, args ...string) (string, error) {
			b, err := exec.Command(name, args...).CombinedOutput()
			return string(b), err
		},
		runAttached: func(name string, args ...string) error {
			c := exec.Command(name, args...)
			c.Stdout, c.Stderr, c.Stdin = os.Stdout, os.Stderr, os.Stdin
			return c.Run()
		},
		exists:         func(p string) bool { _, err := os.Stat(p); return err == nil },
		writeFile:      os.WriteFile,
		mkdirAll:       os.MkdirAll,
		hasCloudflared: func() bool { _, err := exec.LookPath("cloudflared"); return err == nil },
	}
}

func newCFCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cf",
		Short: "Cloudflare tunnel operations (EU node)",
	}
	var tunnelName, tunnelHost, subHost string
	var dryRun bool
	setup := &cobra.Command{
		Use:   "setup",
		Short: "Zero-touch Cloudflare tunnel bring-up: login → create → config → route DNS → service",
		Long: "Collapses the ~12 manual, order-sensitive steps of the Cloudflare bring-up into\n" +
			"one idempotent command, verifying each DNS route actually reaches this tunnel\n" +
			"so a silent `route dns` no-op can't ship a dead endpoint.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			env := realCFEnv(*dir, cmd.OutOrStdout(), dryRun)
			return runCFSetup(env, cfOptions{TunnelName: tunnelName, TunnelHost: tunnelHost, SubHost: subHost})
		},
	}
	setup.Flags().StringVar(&tunnelName, "name", "rdda", "cloudflared tunnel name")
	setup.Flags().StringVar(&tunnelHost, "tunnel-host", "", "hostname for the RU→EU data hop (default: config's cf-tunnel-host)")
	setup.Flags().StringVar(&subHost, "sub-host", "", "hostname for the subscription endpoint (default: config's cf-sub-host)")
	setup.Flags().BoolVar(&dryRun, "dry-run", false, "print the plan without changing anything")
	cmd.AddCommand(setup)
	return cmd
}

type cfOptions struct {
	TunnelName string
	TunnelHost string
	SubHost    string
}

func (e cfEnv) logf(format string, a ...any) { fmt.Fprintf(e.out, format+"\n", a...) }

// runCFSetup drives the bring-up. It is idempotent: a re-run reuses an existing
// tunnel, and DNS routing is verified (not assumed) against this tunnel.
func runCFSetup(e cfEnv, opts cfOptions) error {
	s, err := state.Open(e.dir)
	if err != nil {
		return err
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config (run rdda init first): %w", err)
	}
	// Hostnames: flags override, else fall back to what init stored.
	if opts.TunnelHost == "" {
		opts.TunnelHost = cfg.Cloudflare.TunnelHostname
	}
	if opts.SubHost == "" {
		opts.SubHost = cfg.Cloudflare.SubHostname
	}
	if opts.TunnelHost == "" || opts.SubHost == "" {
		return fmt.Errorf("need both --tunnel-host and --sub-host (or set them via rdda init --cf-tunnel-host/--cf-sub-host)")
	}
	if !e.dryRun && !e.hasCloudflared() {
		return fmt.Errorf("cloudflared not found on PATH — install it first (see install-eu.md)")
	}
	if e.dryRun {
		e.logf("[dry-run] plan for tunnel %q → %s (data), %s (sub):", opts.TunnelName, opts.TunnelHost, opts.SubHost)
	}

	// 1. Login (idempotent: skip if the origin cert already exists).
	certPath := filepath.Join(os.Getenv("HOME"), ".cloudflared", "cert.pem")
	switch {
	case e.exists(certPath):
		e.logf("login: already authenticated (%s)", certPath)
	case e.dryRun:
		e.logf("[dry-run] would run: cloudflared tunnel login")
	default:
		e.logf("login: opening browser for cloudflared tunnel login…")
		if err := e.runAttached("cloudflared", "tunnel", "login"); err != nil {
			return fmt.Errorf("cloudflared tunnel login: %w", err)
		}
	}

	// 2. Create or reuse the tunnel.
	id, creds, err := e.ensureTunnel(opts.TunnelName)
	if err != nil {
		return err
	}
	e.logf("tunnel: %s (%s)", opts.TunnelName, id)

	// 3. Stage credentials + render config into /etc/cloudflared.
	dstCreds := filepath.Join(cloudflaredDir, id+".json")
	if err := e.stageCloudflared(cfg, id, creds, dstCreds, opts); err != nil {
		return err
	}

	// 4. Persist the CF block so render eu/ru + doctor agree with the tunnel.
	cfg.Cloudflare = state.Cloudflare{
		TunnelHostname:  opts.TunnelHost,
		SubHostname:     opts.SubHost,
		TunnelID:        id,
		CredentialsFile: dstCreds,
	}
	if e.dryRun {
		e.logf("[dry-run] would write cloudflare block to %s/config.yaml", e.dir)
	} else if err := s.SaveConfig(cfg); err != nil {
		return err
	}

	// 5. Route DNS for both hostnames and VERIFY each reaches this tunnel.
	var conflicts []string
	for _, h := range []string{opts.TunnelHost, opts.SubHost} {
		res, err := e.routeDNS(opts.TunnelName, h)
		if err != nil {
			return err
		}
		switch {
		case res.Conflict:
			conflicts = append(conflicts, h)
			e.logf("route %s: CONFLICT — %s", h, res.Msg)
		case res.OK:
			e.logf("route %s: %s", h, res.Msg)
		default:
			e.logf("route %s: %s", h, res.Msg)
		}
	}

	// 6. Install + enable the service (unless a conflict left an endpoint dead).
	if len(conflicts) > 0 {
		return fmt.Errorf("DNS conflict on %s: delete the stale record(s) in the Cloudflare dashboard, then re-run — refusing to enable a tunnel with an unrouted hostname", strings.Join(conflicts, ", "))
	}
	if err := e.enableService(); err != nil {
		return err
	}
	e.logf("cloudflared up. Next: re-render EU (loopback) + RU (dials %s), verify, then close inbound 443.", opts.TunnelHost)
	return nil
}

func (e cfEnv) ensureTunnel(name string) (id, creds string, err error) {
	if e.dryRun {
		e.logf("[dry-run] would run: cloudflared tunnel create %s (or reuse if it exists)", name)
		return "dry-run-tunnel-id", "", nil
	}
	out, cErr := e.run("cloudflared", "tunnel", "create", name)
	if cErr == nil {
		return cfsetup.ParseCreate(out)
	}
	// Already exists (or another error): try to find it in the list.
	list, lErr := e.run("cloudflared", "tunnel", "list")
	if lErr != nil {
		return "", "", fmt.Errorf("tunnel create failed (%v) and list failed (%v)", cErr, lErr)
	}
	id, fErr := cfsetup.FindTunnelID(list, name)
	if fErr != nil {
		return "", "", fmt.Errorf("tunnel create failed and %q not found in existing tunnels: %v", name, cErr)
	}
	// Reused tunnel: creds live at the default path next to the origin cert.
	creds = filepath.Join(os.Getenv("HOME"), ".cloudflared", id+".json")
	return id, creds, nil
}

func (e cfEnv) stageCloudflared(cfg state.Config, id, srcCreds, dstCreds string, opts cfOptions) error {
	cfg.Cloudflare = state.Cloudflare{TunnelHostname: opts.TunnelHost, SubHostname: opts.SubHost, TunnelID: id, CredentialsFile: dstCreds}
	ymlBytes, err := cfconfig.Render(cfg, 8080)
	if err != nil {
		return fmt.Errorf("render cloudflared config: %w", err)
	}
	if e.dryRun {
		e.logf("[dry-run] would mkdir %s, copy creds %s → %s (0600, cloudflared user), write config.yml", cloudflaredDir, srcCreds, dstCreds)
		return nil
	}
	if err := e.mkdirAll(cloudflaredDir, 0o755); err != nil {
		return err
	}
	if srcCreds != "" && srcCreds != dstCreds {
		data, err := os.ReadFile(srcCreds)
		if err != nil {
			return fmt.Errorf("read tunnel credentials %s: %w", srcCreds, err)
		}
		if err := e.writeFile(dstCreds, data, 0o600); err != nil {
			return err
		}
	}
	if err := e.writeFile(filepath.Join(cloudflaredDir, "config.yml"), ymlBytes, 0o644); err != nil {
		return err
	}
	// Best-effort: a dedicated service user that can read only its creds dir.
	_, _ = e.run("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", "cloudflared")
	if _, err := e.run("chown", "-R", "cloudflared:cloudflared", cloudflaredDir); err != nil {
		e.logf("warning: chown %s to cloudflared failed: %v", cloudflaredDir, err)
	}
	return nil
}

func (e cfEnv) routeDNS(tunnelName, hostname string) (cfsetup.RouteResult, error) {
	if e.dryRun {
		e.logf("[dry-run] would run + verify: cloudflared tunnel route dns %s %s", tunnelName, hostname)
		return cfsetup.RouteResult{OK: true, Msg: "[dry-run]"}, nil
	}
	out, err := e.run("cloudflared", "tunnel", "route", "dns", tunnelName, hostname)
	return cfsetup.ClassifyRouteDNS(out, err), nil
}

func (e cfEnv) enableService() error {
	if e.dryRun {
		e.logf("[dry-run] would install cloudflared.service, daemon-reload, enable --now cloudflared")
		return nil
	}
	if _, err := e.run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	if _, err := e.run("systemctl", "enable", "--now", "cloudflared"); err != nil {
		return fmt.Errorf("enable cloudflared service: %w", err)
	}
	return nil
}
