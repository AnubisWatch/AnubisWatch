package main

import (
	"fmt"
	"os"
	"sync"
)

const (
	defaultLogMaxSizeMB  = 100
	defaultLogMaxBackups = 5
)

// rotatingFile is an append-only log file that rotates by size: once a write
// would take it past maxSize, anubis.log becomes anubis.log.1, earlier
// backups shift up by one, and the oldest beyond maxBackups is removed.
// Without it, logging.output file|both grew without bound.
type rotatingFile struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
}

func openRotatingFile(path string, maxSizeMB, maxBackups int) (*rotatingFile, error) {
	if maxSizeMB <= 0 {
		maxSizeMB = defaultLogMaxSizeMB
	}
	if maxBackups <= 0 {
		maxBackups = defaultLogMaxBackups
	}
	r := &rotatingFile{path: path, maxSize: int64(maxSizeMB) << 20, maxBackups: maxBackups}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	// #nosec G304 -- operator-configured log path.
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.file = f
	r.size = info.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return 0, os.ErrClosed
	}
	// An entry larger than the limit still goes out whole, into a fresh file.
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, fmt.Errorf("rotate log file: %w", err)
		}
	}
	n, err := r.file.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate closes the current file before renaming it, which Windows requires.
func (r *rotatingFile) rotate() error {
	if err := r.file.Close(); err != nil {
		return err
	}
	r.file = nil
	if err := os.Remove(r.backupName(r.maxBackups)); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := r.maxBackups - 1; i >= 1; i-- {
		if err := os.Rename(r.backupName(i), r.backupName(i+1)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(r.path, r.backupName(1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return r.open()
}

func (r *rotatingFile) backupName(i int) string {
	return fmt.Sprintf("%s.%d", r.path, i)
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
