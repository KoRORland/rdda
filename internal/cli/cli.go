package cli

import (
	"fmt"

	"github.com/KoRORland/rdda/internal/keys"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

const Version = "0.1.0-dev"

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
	return root
}

func newInitCmd(dir *string) *cobra.Command {
	var ruHost, euHost, clientSNI, tunnelSNI string
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
	return cmd
}

func Execute() error { return newRoot().Execute() }
