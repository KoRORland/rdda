package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/KoRORland/rdda/internal/controlchannel"
	"github.com/KoRORland/rdda/internal/state"
	"github.com/spf13/cobra"
)

func newControlChannelCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "control-channel",
		Aliases: []string{"cc"},
		Short:   "Provision the RU→EU control channel (pull-sync + health beat)",
	}
	cmd.AddCommand(newCCShowCmd(dir), newCCInitCmd(dir))
	return cmd
}

// newCCShowCmd (run on EU) prints the pull.env the RU node needs, plus the exact
// `control-channel init` command to run there — so the operator copies one line
// instead of hand-transcribing the token and two URLs (G4).
func newCCShowCmd(dir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the RU control-channel config from this EU node's config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := state.Open(*dir)
			if err != nil {
				return err
			}
			cfg, err := s.LoadConfig()
			if err != nil {
				return err
			}
			host := cfg.Cloudflare.SubHostname
			if host == "" {
				return fmt.Errorf("no Cloudflare sub hostname in config.yaml (set cloudflare.sub_hostname / init --cf-sub-host)")
			}
			if cfg.PullToken == "" {
				return fmt.Errorf("no pull_token in config.yaml (re-run rdda init to generate one)")
			}
			env, err := controlchannel.RenderEnv(host, cfg.PullToken)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "# On the RU node, run:\n")
			fmt.Fprintf(out, "rdda control-channel init --sub-host %s --token %s\n\n", host, cfg.PullToken)
			fmt.Fprintf(out, "# (equivalently, %s/%s:)\n%s", *dir, controlchannel.EnvFileName, env)
			return nil
		},
	}
}

// newCCInitCmd (run on RU) writes <dir>/pull.env from a sub host + token, the
// one manual, error-prone step from install-ru.md §4.1.
func newCCInitCmd(dir *string) *cobra.Command {
	var subHost, token string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write the RU pull.env from the EU sub host + pull token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := controlchannel.RenderEnv(subHost, token)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(*dir, 0o700); err != nil {
				return err
			}
			dst := filepath.Join(*dir, controlchannel.EnvFileName)
			if err := writeControlEnv(dst, env); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "wrote %s\n", dst)
			fmt.Fprintln(out, "next: sudo systemctl enable --now rdda-pull.timer rdda-health.timer")
			return nil
		},
	}
	cmd.Flags().StringVar(&subHost, "sub-host", "", "EU Cloudflare sub hostname (e.g. sub.example.com) (required)")
	cmd.Flags().StringVar(&token, "token", "", "pull token from the EU config.yaml (required)")
	_ = cmd.MarkFlagRequired("sub-host")
	_ = cmd.MarkFlagRequired("token")
	return cmd
}

// writeControlEnv atomically writes the pull.env with 0600 perms and, when the
// rdda service user exists, chowns it to rdda:rdda so rdda-pull/rdda-health
// (User=rdda) can read it. A failed chown (e.g. no rdda user on a dev box) is a
// warning, not a fatal — the file is still written.
func writeControlEnv(dst, content string) error {
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	if u, err := user.Lookup("rdda"); err == nil {
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if err := os.Chown(tmp, uid, gid); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not chown %s to rdda: %v (chown it manually so rdda-pull can read it)\n", dst, err)
		}
	}
	return os.Rename(tmp, dst)
}
