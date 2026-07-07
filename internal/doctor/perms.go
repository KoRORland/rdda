package doctor

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// serviceUserCanRead reports whether a process running as (svcUID, svcGID) can
// read a file owned by (fUID, fGID) with mode. It mirrors the kernel's owner →
// group → other permission precedence.
func serviceUserCanRead(fUID, fGID int, mode fs.FileMode, svcUID, svcGID int) bool {
	p := mode.Perm()
	switch {
	case svcUID == fUID:
		return p&0o400 != 0
	case svcGID == fGID:
		return p&0o040 != 0
	default:
		return p&0o004 != 0
	}
}

// permsCheck verifies the rdda service user can read each of the given state
// files. This catches the field foot-gun where a root-owned (or mis-chowned)
// singbox.json / pull.env left rdda-singbox crash-looping with "permission
// denied" — a failure that looked like a config bug, not an ownership one.
func (d *Doctor) permsCheck(files ...string) Check {
	uid, gid, err := d.svcUser()
	if err != nil {
		return Check{"permissions", WARN, "rdda service user not found; cannot verify state-dir ownership", ""}
	}
	for _, f := range files {
		path := filepath.Join(d.dir, f)
		fuid, fgid, mode, err := d.statFile(path)
		if err != nil {
			continue // absent files are reported by the checks that need them
		}
		if !serviceUserCanRead(fuid, fgid, mode, uid, gid) {
			return Check{"permissions", FAIL,
				fmt.Sprintf("%s not readable by the rdda service user", path),
				"sudo chown rdda:rdda " + path + "  (a root-owned config crash-loops rdda-singbox)"}
		}
	}
	return Check{"permissions", PASS, "state files readable by the rdda service user", ""}
}

func realServiceUser() (int, int, error) {
	u, err := user.Lookup("rdda")
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func realStatFile(path string) (int, int, fs.FileMode, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, 0, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		// Non-Unix: no owner info. Treat as readable (mode carries what we can see).
		return -1, -1, fi.Mode(), nil
	}
	return int(st.Uid), int(st.Gid), fi.Mode(), nil
}
