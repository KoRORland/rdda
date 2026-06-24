// Package pull fetches the RU node's desired xray config from the EU node
// (over Cloudflare) and atomically installs it. A fetch failure leaves the
// existing config in place — the node never fails open.
package pull

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Options configures a single pull.
type Options struct {
	URL    string       // EU /ru/config endpoint (no query string)
	Token  string       // pull token
	Dest   string       // path to write the rendered xray config
	Client *http.Client // optional; defaults to a 30s-timeout client
	Reload func() error // optional; called after a successful swap
}

// Run performs one pull: fetch, validate, atomic write, reload.
func Run(opts Options) error {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	u, err := url.Parse(opts.URL)
	if err != nil {
		return fmt.Errorf("bad pull URL: %w", err)
	}
	q := u.Query()
	q.Set("token", opts.Token)
	u.RawQuery = q.Encode()

	resp, err := client.Get(u.String())
	if err != nil {
		return fmt.Errorf("pull fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull fetch: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("pull read failed: %w", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return fmt.Errorf("pull body is not a JSON object: %w", err)
	}

	dir := filepath.Dir(opts.Dest)
	tmp, err := os.CreateTemp(dir, ".xray-*.json")
	if err != nil {
		return fmt.Errorf("pull temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("pull write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("pull close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("pull chmod: %w", err)
	}
	if err := os.Rename(tmpName, opts.Dest); err != nil {
		return fmt.Errorf("pull rename: %w", err)
	}
	if opts.Reload != nil {
		return opts.Reload()
	}
	return nil
}
