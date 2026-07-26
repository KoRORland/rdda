// Package state is RDDA's plain-file source of truth: config.yaml + clients/.
package state

import (
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Reality holds REALITY parameters for one hop.
type Reality struct {
	Target     string   `yaml:"target"`
	ServerName string   `yaml:"server_name"`
	PrivateKey string   `yaml:"private_key"`
	PublicKey  string   `yaml:"public_key"`
	ShortIDs   []string `yaml:"short_ids"`
}

// Cloudflare holds the EU-side Cloudflare Tunnel parameters. An empty
// TunnelHostname means Cloudflare fronting is disabled (v0.1 REALITY behavior).
type Cloudflare struct {
	TunnelHostname  string `yaml:"tunnel_hostname"`
	SubHostname     string `yaml:"sub_hostname"`
	TunnelID        string `yaml:"tunnel_id"`
	CredentialsFile string `yaml:"credentials_file"`
}

// Desync configures the RU-node nfqws2 (zapret2) egress DPI-desync. It is
// fail-open: a desync failure must not break the tunnel path.
type Desync struct {
	Enabled bool   `yaml:"enabled"`
	Profile string `yaml:"profile"`
	// Fooling is the nfqws2 --dpi-desync-fooling method (e.g. badsum, md5sig).
	// Without it the injected fake packet reaches the real server and corrupts
	// every TLS handshake — including to unblocked hosts and the tunnel/decoy —
	// which is exactly what broke the RU node in the field. Defaults to badsum.
	Fooling string `yaml:"fooling"`
	Ports   []int  `yaml:"ports"`
}

// Alert configures EU-side email alerting via msmtp. Fail-soft; EU node only.
type Alert struct {
	Enabled      bool   `yaml:"enabled"`
	Email        string `yaml:"email"`
	Command      string `yaml:"command"`        // default "msmtp"
	CertWarnDays int    `yaml:"cert_warn_days"` // default 14
}

// Config is the full RDDA deployment description (the EU source of truth).
type Config struct {
	RUHost           string   `yaml:"ru_host"`
	RUPort           int      `yaml:"ru_port"`
	EUHost           string   `yaml:"eu_host"`
	EUPort           int      `yaml:"eu_port"`
	ClientPath       string   `yaml:"client_path"`
	TunnelPath       string   `yaml:"tunnel_path"`
	TunnelUUID       string   `yaml:"tunnel_uuid"`
	SubBaseURL       string   `yaml:"sub_base_url"`
	IntlAllowDomains []string `yaml:"intl_allow_domains"`
	// GeoIPPath is the local path to the geoip-ru rule-set (.srs) on the RU node.
	// When set, RU split-routes domestic (RU) IPs direct and tunnels the rest. It
	// is a LOCAL file (shipped at install time) so the RU data plane never blocks
	// startup on a remote download. Empty disables geoip split-routing (all
	// traffic goes through the EU tunnel — safe, just less efficient).
	GeoIPPath     string     `yaml:"geoip_path"`
	ClientReality Reality    `yaml:"client_reality"`
	TunnelReality Reality    `yaml:"tunnel_reality"`
	Cloudflare    Cloudflare `yaml:"cloudflare"`
	PullToken     string     `yaml:"pull_token"`
	Fingerprint   string     `yaml:"fingerprint"`
	// ProfileTitle is the name Hiddify shows for the client profile. It is a
	// greppable artifact on every user's device and in every QR/screenshot, so
	// it must NOT be the project name (that would tie an inspected device to this
	// specific tool). Empty ⇒ a neutral generic default (see ProfileTitleOr).
	ProfileTitle string `yaml:"profile_title"`
	Desync       Desync `yaml:"desync"`
	Alert        Alert  `yaml:"alert"`
}

// ProfileTitleOr returns the operator-configured client profile name, or a
// neutral generic default when unset — deliberately not the project name.
func (c Config) ProfileTitleOr() string {
	if c.ProfileTitle == "" {
		return "VPN"
	}
	return c.ProfileTitle
}

// Store is a directory-backed RDDA state store.
type Store struct{ dir string }

// Open returns a Store rooted at dir, creating the clients/ subdir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "clients"), 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) configPath() string { return filepath.Join(s.dir, "config.yaml") }

// SaveConfig writes config.yaml.
func (s *Store) SaveConfig(c Config) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath(), b, 0o600)
}

// LoadConfig reads config.yaml.
func (s *Store) LoadConfig() (Config, error) {
	b, err := os.ReadFile(s.configPath())
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	if c.RUHost == "" {
		return Config{}, errors.New("config.yaml: ru_host is empty")
	}
	return c, nil
}

// CFEnabled reports whether the RU→EU hop should go through Cloudflare.
func (c Config) CFEnabled() bool { return c.Cloudflare.TunnelHostname != "" }

// NfqwsArgs returns the nfqws2 command-line flags for the RU node, always
// including a fooling method so the desync never breaks unblocked-host or
// tunnel handshakes (the G2 field failure). Profile and fooling fall back to
// safe defaults.
func (d Desync) NfqwsArgs() string {
	profile := d.Profile
	if profile == "" {
		profile = "fake,split2"
	}
	fooling := d.Fooling
	if fooling == "" {
		fooling = "badsum"
	}
	return fmt.Sprintf("--qnum=200 --dpi-desync=%s --dpi-desync-fooling=%s", profile, fooling)
}

// FP returns the uTLS fingerprint to mimic; defaults to a non-Chrome profile
// (mimicking Chrome is itself a DPI flag under the June-2026 scheme).
func (c Config) FP() string {
	if c.Fingerprint == "" {
		return "firefox"
	}
	return c.Fingerprint
}

// --- Per-node structural variance (D-4) ---
//
// The per-client uTLS pool already diversifies the ClientHello, but the
// multiplex parameters and client routing-rule order were identical on every
// deployment — in combination a structural fingerprint that survives the TLS
// randomization, so characterizing one node generalizes to the whole class.
// These derive those otherwise-constant structural values per deployment from
// the tunnel UUID (unique per init, present in every config, stable across
// re-renders). The point is spreading deployments across buckets, not
// uniqueness; a small parameter space is fine. Every value stays end-to-end
// valid: inbounds accept any multiplex protocol, and the reordered rules are
// equivalent, so nothing needs the two ends to agree on the specific choice.

// structSeed is a stable per-deployment seed. A zero UUID (unconfigured/test)
// yields 0, which the accessors map to the historical defaults.
func (c Config) structSeed() uint64 {
	if c.TunnelUUID == "" {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(c.TunnelUUID))
	return h.Sum64()
}

// muxProtocols are the sing-box multiplex protocols; all three negotiate
// end-to-end (the inbound side just enables multiplex and accepts any).
var muxProtocols = []string{"h2mux", "smux", "yamux"}

// MuxProtocol returns this deployment's multiplex protocol (default h2mux).
func (c Config) MuxProtocol() string {
	s := c.structSeed()
	if s == 0 {
		return "h2mux"
	}
	return muxProtocols[s%uint64(len(muxProtocols))]
}

// MuxStreams returns this deployment's multiplex max_streams, in 4..16 — a
// range sing-box negotiates cleanly (default 8).
func (c Config) MuxStreams() int {
	s := c.structSeed()
	if s == 0 {
		return 8
	}
	return 4 + int((s>>8)%13) // 4..16
}

// RuleOrder returns a stable per-deployment permutation of 0..n-1, used to vary
// the order of equivalent client routing rules. A zero seed returns identity.
func (c Config) RuleOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	s := c.structSeed()
	if s == 0 {
		return order
	}
	// Fisher–Yates driven by an LCG stepped from the seed.
	for i := n - 1; i > 0; i-- {
		s = s*6364136223846793005 + 1442695040888963407
		j := int((s >> 33) % uint64(i+1))
		order[i], order[j] = order[j], order[i]
	}
	return order
}
