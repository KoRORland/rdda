package state

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// ServiceUser is the system user the rdda data-plane/sub services run as.
const ServiceUser = "rdda"

// lookupServiceUser resolves the rdda service user's uid/gid. ok is false when
// the user does not exist (dev/test hosts), so callers no-op instead of erroring.
func lookupServiceUser() (uid, gid int, ok bool) {
	u, err := user.Lookup(ServiceUser)
	if err != nil {
		return 0, 0, false
	}
	uid, err = strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, false
	}
	gid, err = strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, false
	}
	return uid, gid, true
}

// chownToService best-effort chowns a single path to the rdda service user. Used
// right after a root-run command writes into the state dir (e.g. `rdda client
// add`), so the file stays readable by the rdda-sub / rdda-singbox service user.
// Silent no-op when the rdda user is absent or the chown isn't permitted.
func chownToService(path string) {
	if uid, gid, ok := lookupServiceUser(); ok {
		_ = os.Chown(path, uid, gid)
	}
}

// ChownTree recursively chowns the whole state dir to the rdda service user. It
// is the belt-and-suspenders fix for the recurring foot-gun where a root-run
// command leaves a root-owned file (e.g. a client JSON) that the rdda service
// user can't read — which surfaces as an opaque HTTP 500 from the sub server.
// No-op when the rdda user doesn't exist; returns the first real chown error.
func (s *Store) ChownTree() error {
	uid, gid, ok := lookupServiceUser()
	if !ok {
		return nil
	}
	return filepath.Walk(s.dir, func(p string, _ fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}
