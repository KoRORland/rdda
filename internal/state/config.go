// Package state is RDDA's plain-file source of truth: config.yaml + clients/.
package state

import (
	"errors"
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
	Ports   []int  `yaml:"ports"`
}

// Config is the full RDDA deployment description (the EU source of truth).
type Config struct {
	RUHost           string     `yaml:"ru_host"`
	RUPort           int        `yaml:"ru_port"`
	EUHost           string     `yaml:"eu_host"`
	EUPort           int        `yaml:"eu_port"`
	ClientPath       string     `yaml:"client_path"`
	TunnelPath       string     `yaml:"tunnel_path"`
	TunnelUUID       string     `yaml:"tunnel_uuid"`
	SubBaseURL       string     `yaml:"sub_base_url"`
	IntlAllowDomains []string   `yaml:"intl_allow_domains"`
	ClientReality    Reality    `yaml:"client_reality"`
	TunnelReality    Reality    `yaml:"tunnel_reality"`
	Cloudflare       Cloudflare `yaml:"cloudflare"`
	PullToken        string     `yaml:"pull_token"`
	Fingerprint      string     `yaml:"fingerprint"`
	Desync           Desync     `yaml:"desync"`
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

// FP returns the uTLS fingerprint to mimic; defaults to a non-Chrome profile
// (mimicking Chrome is itself a DPI flag under the June-2026 scheme).
func (c Config) FP() string {
	if c.Fingerprint == "" {
		return "firefox"
	}
	return c.Fingerprint
}
