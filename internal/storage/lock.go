package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDataDirLocked is returned by NewEngine when another process (normally a
// running server) already has the data directory open. Two engines on one
// directory would each rewrite wal.log during recovery and orphan the other's
// writes, so the second opener must be refused.
var ErrDataDirLocked = errors.New("data directory is in use by another AnubisWatch process")

const lockFileName = "LOCK"

// acquireDirLock opens <dir>/LOCK and takes an exclusive lock on it.
func acquireDirLock(dir string) (*os.File, error) {
	path := filepath.Join(dir, lockFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, ErrDataDirLocked) {
			return nil, fmt.Errorf("%w (%s)", err, dir)
		}
		return nil, fmt.Errorf("failed to lock data directory: %w", err)
	}
	return f, nil
}
