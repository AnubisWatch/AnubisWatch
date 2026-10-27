package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRotatingFileRotatesBySizeAndKeepsBackups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.log")
	r, err := openRotatingFile(path, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.maxSize = 10 // bytes, so the test needn't write megabytes

	for _, line := range []string{"aaaaaaa\n", "bbbbbbb\n", "ccccccc\n", "ddddddd\n"} {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}

	want := map[string]string{
		path:        "ddddddd\n",
		path + ".1": "ccccccc\n",
		path + ".2": "bbbbbbb\n",
	}
	for p, content := range want {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if string(got) != content {
			t.Errorf("%s = %q, want %q", filepath.Base(p), got, content)
		}
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("backup beyond max_backups kept: %v", err)
	}
}

func TestRotatingFileAppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	r, err := openRotatingFile(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.maxSize != defaultLogMaxSizeMB<<20 || r.maxBackups != defaultLogMaxBackups {
		t.Errorf("defaults = %d bytes / %d backups", r.maxSize, r.maxBackups)
	}
	if _, err := r.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	r.Close()
	got, _ := os.ReadFile(path)
	if string(got) != "old\nnew\n" {
		t.Errorf("file = %q", got)
	}
}

// `anubis logs --follow` keeps following across a rotation instead of
// staring at the renamed file forever.
func TestFollowLogFileContinuesAcrossRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.log")
	r, err := openRotatingFile(path, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.maxSize = 10

	var out followBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- followLogFile(ctx, path, 0, &out) }()

	waitFor := func(text string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(out.String(), text) {
			if time.Now().After(deadline) {
				t.Fatalf("follower never printed %q; got %q", text, out.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	r.Write([]byte("first--\n"))
	// Rotate only once the follower is reading the original file; otherwise
	// a slow start would open the new file and never see the first line.
	waitFor("first--")
	r.Write([]byte("second-\n")) // rotates: "first--" moves to .1
	waitFor("second-")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "first--\nsecond-\n" {
		t.Errorf("followed output = %q", got)
	}
}

type followBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *followBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *followBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A rotation between `anubis logs` reading the file and starting to follow
// it leaves a shorter file behind; following must start at its beginning.
func TestFollowLogFileStartsOverOnShorterFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anubis.log")
	if err := os.WriteFile(path, []byte("new\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	var out followBuffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- followLogFile(ctx, path, 1000, &out) }()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "new") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if out.String() != "new\n" {
		t.Fatalf("followed %q, want the shorter file from its start", out.String())
	}
}
