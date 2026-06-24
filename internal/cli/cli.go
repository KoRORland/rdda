package cli

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/KoRORland/rdda/internal/cfconfig"
	"github.com/KoRORland/rdda/internal/keys"
	"github.com/KoRORland/rdda/internal/pull"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/subserver"
	"github.com/KoRORland/rdda/internal/subscription"
	"github.com/KoRORland/rdda/internal/xrayconf"
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
	root.AddCommand(newPullCmd(&dir))
	return root
}

func newInitCmd(dir *string) *cobra.Command {
	var ruHost, euHost, clientSNI, tunnelSNI string
	var cfTunnelHost, cfSubHost, cfTunnelID, cfCredsFile string
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
				ClientPath: "/cl", TunnelPath: "/tn",
				TunnelUUID: keys.NewUUID(),
				SubBaseURL: "https://" + euHost,
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
	cmd.Flags().StringVar(&clientSNI, "client-sni", "www.microsoft.com", "REALITY SNI for client→RU hop")
	cmd.Flags().StringVar(&tunnelSNI, "tunnel-sni", "www.apple.com", "REALITY SNI for RU→EU hop")
	_ = cmd.MarkFlagRequired("ru-host")
	_ = cmd.MarkFlagRequired("eu-host")
	cmd.Flags().StringVar(&cfTunnelHost, "cf-tunnel-host", "", "Cloudflare hostname for the RU→EU data hop (optional; enables CF fronting)")
	cmd.Flags().StringVar(&cfSubHost, "cf-sub-host", "", "Cloudflare hostname for the subscription endpoint")
	cmd.Flags().StringVar(&cfTunnelID, "cf-tunnel-id", "", "Cloudflare Tunnel ID")
	cmd.Flags().StringVar(&cfCredsFile, "cf-credentials-file", "", "path to the cloudflared tunnel credentials JSON")
	return cmd
}

func newClientCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{Use: "client", Short: "Manage clients"}

	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add a client and print its vless:// link",
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
			c, err := s.AddClient(args[0])
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), subscription.ClientURI(cfg, c))
			return nil
		},
	}

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
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", c.Name, c.Created.Format("2006-01-02"))
			}
			return nil
		},
	}

	cmd.AddCommand(add, rm, list)
	return cmd
}

func newRenderCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{Use: "render", Short: "Render xray configs"}
	cmd.AddCommand(&cobra.Command{
		Use:   "ru",
		Short: "Render the RU node xray config",
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
			b, err := xrayconf.RenderRU(cfg, clients)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "eu",
		Short: "Render the EU node xray config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			b, err := xrayconf.RenderEU(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			return nil
		},
	})
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

func newPullCmd(dir *string) *cobra.Command {
	var from, token, dest, reloadCmd string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Pull the RU xray config from EU (over Cloudflare) and reload",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if from == "" || token == "" {
				return fmt.Errorf("--from and --token are required")
			}
			var reload func() error
			if reloadCmd != "" {
				reload = func() error {
					parts := strings.Fields(reloadCmd)
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
	cmd.Flags().StringVar(&dest, "dest", "/etc/rdda-ru/xray.json", "destination xray config path")
	cmd.Flags().StringVar(&reloadCmd, "reload-cmd", "systemctl reload-or-restart rdda-xray", "command run after a successful pull")
	return cmd
}

func Execute() error { return newRoot().Execute() }
