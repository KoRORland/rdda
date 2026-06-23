//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"github.com/KoRORland/rdda/internal/state"
	"github.com/KoRORland/rdda/internal/xrayconf"
)

const (
	euPort    = 20443
	ruPort    = 20444
	socksPort = 21080
)

// TestRealDeployTunnel provisions EU and RU exactly like a real deployment — the
// ACTUAL rdda-xray.service systemd units, running as the rdda user, reading
// configs from 0700 /etc/rdda{,-ru} owned by rdda — then routes an HTTP request
// through the 2-hop tunnel (client → RU → EU → internet). A unit/user/permission
// bug (e.g. a service that cannot read its config) makes this test fail.
func TestRealDeployTunnel(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("real-deploy integration test requires root (systemd/useradd/chown)")
	}
	for _, bin := range []string{"xray", "rdda", "systemctl", "jq"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available", bin)
		}
	}

	cmd := exec.Command("bash", filepath.Join(".", "run.sh"), strconv.Itoa(euPort), strconv.Itoa(ruPort))
	out, err := cmd.CombinedOutput()
	t.Logf("run.sh output:\n%s", out)
	t.Cleanup(teardown)
	if err != nil {
		t.Fatalf("real-deploy harness failed: %v", err)
	}

	// Both server units must be active under the real unit + rdda user + perms.
	for _, unit := range []string{"rdda-xray", "rdda-xray-ru"} {
		if st := unitState(unit); st != "active" {
			t.Fatalf("%s not active (state=%s)\n%s", unit, st, journal(unit))
		}
	}

	// Build the client config from the deployed EU state, as a user device would.
	st, err := state.Open("/etc/rdda")
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
	cfg.RUHost = "127.0.0.1"
	cfg.RUPort = ruPort

	clientJSON, err := xrayconf.RenderClient(cfg, clients[0].UUID, socksPort)
	if err != nil {
		t.Fatalf("render client config: %v", err)
	}
	clientCfgPath := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(clientCfgPath, clientJSON, 0o600); err != nil {
		t.Fatalf("write client.json: %v", err)
	}

	var clientLog bytes.Buffer
	clientProc := exec.Command("xray", "run", "-c", clientCfgPath)
	clientProc.Stdout = &clientLog
	clientProc.Stderr = &clientLog
	if err := clientProc.Start(); err != nil {
		t.Fatalf("start client xray: %v", err)
	}
	t.Cleanup(func() { _ = clientProc.Process.Kill() })

	// Let REALITY handshakes settle.
	time.Sleep(3 * time.Second)

	rawDialer, err := proxy.SOCKS5("tcp", "127.0.0.1:"+strconv.Itoa(socksPort), nil, proxy.Direct)
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
		t.Logf("client.log:\n%s", clientLog.String())
		t.Logf("rdda-xray journal:\n%s", journal("rdda-xray"))
		t.Logf("rdda-xray-ru journal:\n%s", journal("rdda-xray-ru"))
		t.Fatalf("request through tunnel failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("empty response through tunnel")
	}
	t.Logf("tunnel response (%d bytes): %s", len(body), body)
}

func unitState(unit string) string {
	out, _ := exec.Command("systemctl", "is-active", unit).Output()
	return strings.TrimSpace(string(out))
}

func journal(unit string) string {
	out, _ := exec.Command("journalctl", "-u", unit, "--no-pager", "-n", "40").CombinedOutput()
	return string(out)
}

func teardown() {
	_ = exec.Command("systemctl", "stop", "rdda-xray.service", "rdda-xray-ru.service").Run()
	_ = os.Remove("/etc/systemd/system/rdda-xray.service")
	_ = os.Remove("/etc/systemd/system/rdda-xray-ru.service")
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = os.RemoveAll("/etc/rdda")
	_ = os.RemoveAll("/etc/rdda-ru")
}
