//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/xrayconf"
)

const (
	euPort   = 20443
	ruPort   = 20444
	socksPort = 21080
)

// TestTwoHopTunnel boots EU+RU xray instances via run.sh, starts a client xray
// with a SOCKS5 inbound, and confirms an HTTP request routes through the 2-hop
// tunnel (client → RU → EU → internet).
func TestTwoHopTunnel(t *testing.T) {
	if _, err := exec.LookPath("xray"); err != nil {
		t.Skip("xray binary not installed")
	}
	if _, err := exec.LookPath("rdda"); err != nil {
		t.Skip("rdda binary not installed")
	}

	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	// Start EU and RU via run.sh.
	runSh := filepath.Join(testDir(), "run.sh")
	cmd := exec.Command("bash", runSh, dir, "20443", "20444")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}

	// Kill EU+RU on cleanup. pids file has one PID per line; use tr to convert
	// newlines to spaces so all PIDs are passed to a single kill invocation.
	t.Cleanup(func() {
		pidsFile := filepath.Join(dir, "pids")
		_ = exec.Command("bash", "-c", "kill $(tr '\\n' ' ' < "+pidsFile+") 2>/dev/null").Run()
	})

	// Load state to build client xray config.
	st, err := state.Open(stateDir)
	if err != nil {
		t.Fatalf("open state: %v", err)
	}
	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	clients, err := st.ListClients()
	if err != nil || len(clients) == 0 {
		t.Fatalf("list clients: %v (count=%d)", err, len(clients))
	}

	// Patch RU port into config for the client outbound.
	cfg.RUPort = ruPort

	clientJSON, err := xrayconf.RenderClient(cfg, clients[0].UUID, socksPort)
	if err != nil {
		t.Fatalf("render client config: %v", err)
	}
	clientCfgPath := filepath.Join(dir, "client.json")
	if err := os.WriteFile(clientCfgPath, clientJSON, 0o600); err != nil {
		t.Fatalf("write client.json: %v", err)
	}
	t.Logf("client.json:\n%s", prettyJSON(clientJSON))

	// Start client xray.
	clientProc := exec.Command("xray", "run", "-c", clientCfgPath)
	clientLog, _ := os.Create(filepath.Join(dir, "client.log"))
	clientProc.Stdout = clientLog
	clientProc.Stderr = clientLog
	if err := clientProc.Start(); err != nil {
		t.Fatalf("start client xray: %v", err)
	}
	t.Cleanup(func() {
		_ = clientProc.Process.Kill()
		_ = clientLog.Close()
	})

	// Wait for all three instances to settle.
	time.Sleep(3 * time.Second)

	// Build http.Client that dials through the SOCKS5 proxy.
	rawDialer, err := proxy.SOCKS5("tcp", "127.0.0.1:21080", nil, proxy.Direct)
	if err != nil {
		t.Fatalf("create SOCKS5 dialer: %v", err)
	}
	ctxDialer, ok := rawDialer.(proxy.ContextDialer)
	if !ok {
		t.Fatal("SOCKS5 dialer does not implement proxy.ContextDialer")
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return ctxDialer.DialContext(ctx, network, addr)
		},
	}
	httpClient := &http.Client{Transport: tr, Timeout: 15 * time.Second}

	resp, err := httpClient.Get("http://detectportal.firefox.com/success.txt")
	if err != nil {
		// Dump logs for diagnosis.
		logDump(t, dir, "eu.log")
		logDump(t, dir, "ru.log")
		logDump(t, dir, "client.log")
		t.Fatalf("request through tunnel failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("empty response through tunnel")
	}
	t.Logf("tunnel response (%d bytes): %s", len(body), body)
}

// testDir returns the directory containing this test file at runtime.
func testDir() string {
	// The test binary's working directory is the package directory.
	return "."
}

func prettyJSON(b []byte) string {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	return string(out)
}

func logDump(t *testing.T, dir, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err == nil {
		t.Logf("=== %s ===\n%s", name, b)
	}
}
