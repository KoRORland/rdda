package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"github.com/KoRORland/rdda/internal/cfconfig"
	"github.com/KoRORland/rdda/internal/keys"
	"github.com/KoRORland/rdda/internal/pull"
	"github.com/KoRORland/rdda/internal/qr"
	"github.com/KoRORland/rdda/internal/shellword"
	"github.com/KoRORland/rdda/internal/singboxconf"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/subscription"
	"github.com/KoRORland/rdda/internal/subserver"
	"github.com/spf13/cobra"
)

// Version is the RDDA release, injected at build time via
// -ldflags "-X github.com/KoRORland/rdda/internal/cli.Version=<tag>".
// Local/dev builds report "dev".
var Version = "dev"

func newRoot() *cobra.Command {
	var dir string
	root := &cobra.Command{
		Use:           "rdda",
		Short:         "RDDA — Russian Doll Double Agent control CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&dir, "dir", "/etc/rdda", "RDDA state directory")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the rdda version",
		Run:   func(cmd *cobra.Command, _ []string) { fmt.Fprintln(cmd.OutOrStdout(), Version) },
	})
	root.AddCommand(newInitCmd(&dir))
	root.AddCommand(newClientCmd(&dir))
	root.AddCommand(newRenderCmd(&dir))
	root.AddCommand(newServeCmd(&dir))
	root.AddCommand(newPullCmd())
	root.AddCommand(newControlChannelCmd(&dir))
	root.AddCommand(newCFCmd(&dir))
	root.AddCommand(newRouteCmd())
	root.AddCommand(newCheckDestCmd())
	root.AddCommand(newHealthCmd())
	root.AddCommand(newStatusCmd(&dir))
	root.AddCommand(newBackupCmd(&dir))
	root.AddCommand(newRestoreCmd(&dir))
	root.AddCommand(newDoctorCmd(&dir))
	root.AddCommand(newAlertCmd(&dir))
	root.AddCommand(newUpdateCmd(&dir))
	root.AddCommand(newHealCmd())
	return root
}

func newInitCmd(dir *string) *cobra.Command {
	var ruHost, euHost, clientSNI, tunnelSNI string
	var cfTunnelHost, cfSubHost, cfTunnelID, cfCredsFile string
	var fp, geoipPath string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate keys and write config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cr, err := keys.NewX25519Keypair()
			if err != nil {
				return err
			}
			tr, err := keys.NewX25519Keypair()
			if err != nil {
				return err
			}
			csid, err := keys.NewShortID()
			if err != nil {
				return err
			}
			tsid, err := keys.NewShortID()
			if err != nil {
				return err
			}
			pullTok, err := keys.NewToken()
			if err != nil {
				return err
			}
			cfg := state.Config{
				RUHost: ruHost, RUPort: 443, EUHost: euHost, EUPort: 443,
				Fingerprint: fp,
				ClientPath:  "/cl", TunnelPath: "/tn",
				TunnelUUID: keys.NewUUID(),
				SubBaseURL: "https://" + euHost,
				GeoIPPath:  geoipPath,
				ClientReality: state.Reality{
					Target: clientSNI + ":443", ServerName: clientSNI,
					PrivateKey: cr.PrivateKey, PublicKey: cr.PublicKey, ShortIDs: []string{csid},
				},
				TunnelReality: state.Reality{
					Target: tunnelSNI + ":443", ServerName: tunnelSNI,
					PrivateKey: tr.PrivateKey, PublicKey: tr.PublicKey, ShortIDs: []string{tsid},
				},
				Cloudflare: state.Cloudflare{
					TunnelHostname:  cfTunnelHost,
					SubHostname:     cfSubHost,
					TunnelID:        cfTunnelID,
					CredentialsFile: cfCredsFile,
				},
				PullToken: pullTok,
			}
			if err := s.SaveConfig(cfg); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote config to %s/config.yaml\n", *dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&ruHost, "ru-host", "", "RU node public host/IP (required)")
	cmd.Flags().StringVar(&euHost, "eu-host", "", "EU node public host/IP (required)")
	cmd.Flags().StringVar(&clientSNI, "client-sni", "addons.mozilla.org", "REALITY SNI/dest for client→RU hop (must be reachable + unblocked FROM the RU node)")
	cmd.Flags().StringVar(&tunnelSNI, "tunnel-sni", "addons.mozilla.org", "REALITY SNI/dest for the non-CF RU→EU fallback (dialed from the EU node)")
	cmd.Flags().StringVar(&geoipPath, "geoip-path", "/etc/rdda/geoip-ru.srs", "RU local geoip-ru rule-set path for domestic split-routing (empty to disable)")
	_ = cmd.MarkFlagRequired("ru-host")
	_ = cmd.MarkFlagRequired("eu-host")
	cmd.Flags().StringVar(&cfTunnelHost, "cf-tunnel-host", "", "Cloudflare hostname for the RU→EU data hop (optional; enables CF fronting)")
	cmd.Flags().StringVar(&cfSubHost, "cf-sub-host", "", "Cloudflare hostname for the subscription endpoint")
	cmd.Flags().StringVar(&cfTunnelID, "cf-tunnel-id", "", "Cloudflare Tunnel ID")
	cmd.Flags().StringVar(&cfCredsFile, "cf-credentials-file", "", "path to the cloudflared tunnel credentials JSON")
	cmd.Flags().StringVar(&fp, "fingerprint", "firefox", "uTLS fingerprint to mimic (non-Chrome recommended)")
	return cmd
}

func newClientCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{Use: "client", Short: "Manage clients"}

	var addFP string
	var addConfig bool
	var dataURI bool
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a client and print its Hiddify import QR + link",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			c, err := s.AddClientWithFingerprint(args[0], addFP)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "client %q added (fingerprint: %s)\n", c.Name, c.Fingerprint)
			if err := emitClientImport(cmd, s, cfg, c, dataURI); err != nil {
				return err
			}
			if addConfig {
				body, err := subscription.Build(cfg, c)
				if err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), body)
			}
			return nil
		},
	}
	add.Flags().StringVar(&addFP, "fingerprint", "", "uTLS fingerprint to pin ("+state.FingerprintList()+"); default: random per client")
	add.Flags().BoolVar(&addConfig, "config", false, "also print the raw sing-box config JSON")
	add.Flags().BoolVar(&dataURI, "data-uri", false, "also print a data:image/png URI of the QR (paste into a browser to view/scan from a headless box)")

	qrCmd := &cobra.Command{
		Use:     "qr <name>",
		Aliases: []string{"link"},
		Short:   "Reprint an existing client's Hiddify import QR + link",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			c, err := findClientByName(s, args[0])
			if err != nil {
				return err
			}
			return emitClientImport(cmd, s, cfg, c, dataURI)
		},
	}
	qrCmd.Flags().BoolVar(&dataURI, "data-uri", false, "also print a data:image/png URI of the QR (paste into a browser to view/scan from a headless box)")

	show := &cobra.Command{
		Use:   "show <name>",
		Short: "Show an existing client: details + Hiddify import QR/link + config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			c, err := findClientByName(s, args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "client %q (fingerprint: %s, created: %s)\n",
				c.Name, c.FingerprintOr(cfg.FP()), c.Created.Format("2006-01-02"))
			if err := emitClientImport(cmd, s, cfg, c, dataURI); err != nil {
				return err
			}
			body, err := subscription.Build(cfg, c)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), body)
			return nil
		},
	}
	show.Flags().BoolVar(&dataURI, "data-uri", false, "also print a data:image/png URI of the QR (paste into a browser to view/scan from a headless box)")

	rm := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			return s.RemoveClient(args[0])
		},
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List clients",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			clients, err := s.ListClients()
			if err != nil {
				return err
			}
			for _, c := range clients {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", c.Name, c.FingerprintOr("firefox"), c.Created.Format("2006-01-02"))
			}
			return nil
		},
	}

	cmd.AddCommand(add, qrCmd, show, rm, list)
	return cmd
}

// findClientByName looks up a client by its exact name, returning a friendly
// "not found" error the CLI surfaces directly.
func findClientByName(s *state.Store, name string) (state.Client, error) {
	clients, err := s.ListClients()
	if err != nil {
		return state.Client{}, err
	}
	for _, c := range clients {
		if c.Name == name {
			return c, nil
		}
	}
	return state.Client{}, fmt.Errorf("client %q not found", name)
}

// emitClientImport saves a QR that embeds the client's full sing-box config as
// data — not a link. The payload references only the RU entry node, so it exposes
// nothing about the EU exit and the user imports it offline (no server fetch,
// nothing to time out or reveal). The PNG (chowned to the service user) is the
// artifact the operator hands over. It also renders the QR inline when the
// terminal is wide enough, and, with dataURI, prints a data: URI of the PNG so an
// operator on a headless box can view/scan it via a browser. Shared by `client
// add`, `client qr`, and `client show`.
func emitClientImport(cmd *cobra.Command, s *state.Store, cfg state.Config, c state.Client, dataURI bool) error {
	payload, err := subscription.QRPayload(cfg, c)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	pngPath := s.ClientQRPath(c.Name)
	if err := qr.PNG(payload, pngPath); err != nil {
		return fmt.Errorf("write QR PNG: %w", err)
	}
	s.ChownServiceFile(pngPath)
	fmt.Fprintf(out, "Hiddify QR (offline — holds the config, no server needed): %s\n", pngPath)

	// The full-config payload makes a dense, high-version QR. Render it inline only
	// when the terminal is genuinely wide enough to show it un-wrapped — a wrapped
	// QR is an unscannable mess, which is worse than not showing it at all.
	art, aerr := qr.Terminal(payload)
	switch {
	case aerr == nil && terminalWidth() >= qrArtWidth(art):
		fmt.Fprintln(out, art)
	case !dataURI:
		fmt.Fprintf(out, "(terminal too narrow to show the QR inline — send %s, or re-run with --data-uri)\n", pngPath)
	}

	if dataURI {
		b, err := os.ReadFile(pngPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "data:image/png;base64,%s\n", base64.StdEncoding.EncodeToString(b))
	}
	return nil
}

// terminalWidth returns the width of the controlling terminal in columns, or 0
// when stdout isn't a terminal (piped, redirected, tests) — in which case the
// caller skips the inline QR.
func terminalWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}

// qrArtWidth is the widest line (in runes) of a rendered terminal QR — the number
// of columns it needs to display without wrapping.
func qrArtWidth(art string) int {
	w := 0
	for _, line := range strings.Split(art, "\n") {
		if n := len([]rune(line)); n > w {
			w = n
		}
	}
	return w
}

func newRenderCmd(dir *string) *cobra.Command {
	var clientUUID string
	var socksPort int
	cmd := &cobra.Command{Use: "render", Short: "Render sing-box configs"}
	cmd.AddCommand(&cobra.Command{
		Use:   "ru",
		Short: "Render the RU node sing-box config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			clients, err := s.ListClients()
			if err != nil {
				return err
			}
			b, err := singboxconf.RenderRU(cfg, clients)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "eu",
		Short: "Render the EU node sing-box config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			b, err := singboxconf.RenderEU(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	})
	clientCmd := &cobra.Command{
		Use:   "client",
		Short: "Render a client-side sing-box config (SOCKS inbound → RU)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			b, err := singboxconf.RenderClient(cfg, clientUUID, socksPort)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	}
	clientCmd.Flags().StringVar(&clientUUID, "uuid", "", "client UUID (required)")
	clientCmd.Flags().IntVar(&socksPort, "socks-port", 1080, "local SOCKS inbound port")
	_ = clientCmd.MarkFlagRequired("uuid")
	cmd.AddCommand(clientCmd)
	cmd.AddCommand(&cobra.Command{
		Use:   "cloudflared",
		Short: "Render the cloudflared ingress config (EU node)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			b, err := cfconfig.Render(cfg, 8080)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "nfqws",
		Short: "Print nfqws2 desync flags for the RU node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), cfg.Desync.NfqwsArgs())
			return nil
		},
	})
	return cmd
}

func newServeCmd(dir *string) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the subscription HTTP server (EU node)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rdda subscription server on %s\n", addr)
			return http.ListenAndServe(addr, subserver.Handler(s))
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	return cmd
}

func newPullCmd() *cobra.Command {
	var from, token, dest, reloadCmd string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull the RU sing-box config from EU (over Cloudflare) and reload",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" || token == "" {
				return fmt.Errorf("--from and --token are required")
			}
			var reload func() error
			if reloadCmd != "" {
				parts, err := shellword.Split(reloadCmd)
				if err != nil {
					return fmt.Errorf("bad --reload-cmd: %w", err)
				}
				if len(parts) == 0 {
					return fmt.Errorf("--reload-cmd is empty after parsing")
				}
				reload = func() error {
					return exec.Command(parts[0], parts[1:]...).Run()
				}
			}
			if err := pull.Run(pull.Options{URL: from, Token: token, Dest: dest, Reload: reload}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pulled RU config to %s\n", dest)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "EU /ru/config URL (required)")
	cmd.Flags().StringVar(&token, "token", "", "pull token (required)")
	cmd.Flags().StringVar(&dest, "dest", "/etc/rdda/singbox.json", "destination sing-box config path")
	cmd.Flags().StringVar(&reloadCmd, "reload-cmd", "sudo systemctl reload-or-restart rdda-singbox", "command run after a successful pull")
	return cmd
}

func Execute() error { return newRoot().Execute() }
