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
	Name    string    `json:"name"`
	UUID    string    `json:"uuid"`
	ShortID string    `json:"short_id"`
	Token   string    `json:"token"`
	Created time.Time `json:"created"`
}

func (s *Store) clientsDir() string { return filepath.Join(s.dir, "clients") }

func clientFileName(name string) string { return name + ".json" }

// AddClient creates and persists a new client; errors on empty or duplicate name.
func (s *Store) AddClient(name string) (Client, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Client{}, fmt.Errorf("client name must not be empty")
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
	c := Client{Name: name, UUID: keys.NewUUID(), ShortID: sid, Token: tok, Created: time.Now().UTC()}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return Client{}, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return Client{}, err
	}
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
