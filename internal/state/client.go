package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KoRORland/rdda/internal/keys"
)

// Client is one VPN user.
type Client struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	ShortID string `json:"short_id"`
	Token   string `json:"token"`
	// Fingerprint is the uTLS fingerprint this client mimics on the client→RU
	// hop, randomized per client at creation so the fleet isn't fingerprint-
	// uniform. Empty on clients created before this field existed.
	Fingerprint string    `json:"fingerprint,omitempty"`
	Created     time.Time `json:"created"`
}

// FingerprintOr returns the client's own fingerprint, falling back to the given
// node default for clients created before per-client fingerprints existed.
func (c Client) FingerprintOr(fallback string) string {
	if c.Fingerprint != "" {
		return c.Fingerprint
	}
	return fallback
}

func (s *Store) clientsDir() string { return filepath.Join(s.dir, "clients") }

func clientFileName(name string) string { return name + ".json" }

// ClientQRPath returns where a client's QR PNG lives (next to its JSON).
func (s *Store) ClientQRPath(name string) string {
	return filepath.Join(s.clientsDir(), name+".png")
}

// ChownServiceFile best-effort chowns a state-dir file to the rdda service user,
// for root-run commands (e.g. `rdda client qr`) that write alongside client data.
func (s *Store) ChownServiceFile(path string) { chownToService(path) }

// AddClient creates and persists a new client with a randomized client-hop
// fingerprint; errors on empty or duplicate name.
func (s *Store) AddClient(name string) (Client, error) {
	return s.AddClientWithFingerprint(name, "")
}

// AddClientWithFingerprint is AddClient with an explicit uTLS fingerprint. An
// empty fingerprint is randomized from ClientFingerprints; a non-empty one must
// be a supported pool value.
func (s *Store) AddClientWithFingerprint(name, fingerprint string) (Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Client{}, fmt.Errorf("client name must not be empty")
	}
	if fingerprint == "" {
		fingerprint = RandomClientFingerprint()
	} else if !IsClientFingerprint(fingerprint) {
		return Client{}, fmt.Errorf("unsupported fingerprint %q (choose one of: %s)", fingerprint, FingerprintList())
	}
	path := filepath.Join(s.clientsDir(), clientFileName(name))
	if _, err := os.Stat(path); err == nil {
		return Client{}, fmt.Errorf("client %q already exists", name)
	}
	sid, err := keys.NewShortID()
	if err != nil {
		return Client{}, err
	}
	tok, err := keys.NewToken()
	if err != nil {
		return Client{}, err
	}
	c := Client{Name: name, UUID: keys.NewUUID(), ShortID: sid, Token: tok, Fingerprint: fingerprint, Created: time.Now().UTC()}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Client{}, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return Client{}, err
	}
	// `rdda client add` is run by the operator (root); without this the new file
	// is root-owned 0600 and the rdda-sub service user (User=rdda) can't read it,
	// so /ru/config and /sub/ 500 until a manual chown. Fix it at the source.
	chownToService(path)
	return c, nil
}

// RemoveClient deletes a client file.
func (s *Store) RemoveClient(name string) error {
	path := filepath.Join(s.clientsDir(), clientFileName(name))
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("client %q not found", name)
	}
	return os.Remove(path)
}

// ListClients returns all clients sorted by name.
func (s *Store) ListClients() ([]Client, error) {
	entries, err := os.ReadDir(s.clientsDir())
	if err != nil {
		return nil, err
	}
	var out []Client
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.clientsDir(), e.Name()))
		if err != nil {
			return nil, err
		}
		var c Client
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ClientByToken finds a client by its subscription token.
func (s *Store) ClientByToken(token string) (Client, bool, error) {
	list, err := s.ListClients()
	if err != nil {
		return Client{}, false, err
	}
	for _, c := range list {
		if c.Token == token {
			return c, true, nil
		}
	}
	return Client{}, false, nil
}
