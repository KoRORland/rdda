package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/KoRORland/rdda/internal/health"
)

// RUHealth is the last beat the EU node received from the RU node, plus the
// EU-clock time it arrived (used for staleness — never trust the RU clock).
type RUHealth struct {
	health.Report
	ReceivedAt time.Time `json:"received_at"`
}

func (s *Store) ruHealthPath() string { return filepath.Join(s.dir, "ru-health.json") }

// SaveRUHealth atomically writes the last RU beat.
func (s *Store) SaveRUHealth(h RUHealth) error {
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.ruHealthPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.ruHealthPath())
}

// LoadRUHealth reads the last RU beat; ok=false when none has arrived.
func (s *Store) LoadRUHealth() (RUHealth, bool, error) {
	b, err := os.ReadFile(s.ruHealthPath())
	if os.IsNotExist(err) {
		return RUHealth{}, false, nil
	}
	if err != nil {
		return RUHealth{}, false, err
	}
	var h RUHealth
	if err := json.Unmarshal(b, &h); err != nil {
		return RUHealth{}, false, err
	}
	return h, true, nil
}
