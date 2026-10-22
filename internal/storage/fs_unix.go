//go:build !windows

package storage

import (
	"errors"
	"os"
	"syscall"
)

// lockFile takes an exclusive, non-blocking advisory lock on f. The lock is
// held for as long as f stays open and is released by the kernel if the
// process dies, so a crash never leaves a stale lock behind.
func lockFile(f *os.File) error {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return ErrDataDirLocked
	}
	return err
}

// syncDir fsyncs a directory so a rename inside it survives power loss.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
