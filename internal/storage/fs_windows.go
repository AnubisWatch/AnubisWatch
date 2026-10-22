//go:build windows

package storage

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive, non-blocking lock on f. The lock is held for
// as long as f stays open and is released by the OS if the process dies.
func lockFile(f *os.File) error {
	ol := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrDataDirLocked
	}
	return err
}

// syncDir is a no-op on Windows: NTFS journals renames and directories cannot
// be opened for fsync.
func syncDir(string) error { return nil }
