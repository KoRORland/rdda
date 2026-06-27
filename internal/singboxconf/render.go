// Package singboxconf renders sing-box JSON configs from RDDA state.
package singboxconf

import (
	"strconv"
	"strings"

	"github.com/KoRORland/rdda/internal/state"
)

type obj = map[string]any

// splitHostPort splits "host:port" into (host, port), applying defPort when no
// port is present. A REALITY handshake target is typically "host:443".
func splitHostPort(target string, defPort int) (string, int) {
	h, p, ok := strings.Cut(target, ":")
	if !ok {
		return target, defPort
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, defPort
	}
	return h, n
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

var _ = state.Config{} // keep the import until render funcs land
