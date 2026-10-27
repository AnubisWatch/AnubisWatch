package storage

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func openSnapshotTestDB(t *testing.T, encrypted bool) *CobaltDB {
	t.Helper()
	cfg := core.StorageConfig{Path: t.TempDir()}
	if encrypted {
		cfg.Encryption = core.EncryptionConfig{Enabled: true, Key: strings.Repeat("k", 32)}
	}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func writeFileSnapshot(t *testing.T, s *FileSnapshotStore, index uint64, data []byte) string {
	t.Helper()
	sink, err := s.Create(1, index, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Uneven writes cross frame boundaries.
	for rest := data; len(rest) > 0; {
		n := min(len(rest), 100_003)
		if _, err := sink.Write(rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	return sink.ID()
}

func readFileSnapshot(s *FileSnapshotStore, id string) ([]byte, error) {
	src, err := s.Open(id)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}

func TestFileSnapshotStoreRoundTrip(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, size := range []int{0, 1, snapshotFrameData - 1, snapshotFrameData, 3*snapshotFrameData + 17} {
			db := openSnapshotTestDB(t, encrypted)
			s, err := NewFileSnapshotStore(db)
			if err != nil {
				t.Fatal(err)
			}
			data := make([]byte, size)
			_, _ = rand.Read(data)
			id := writeFileSnapshot(t, s, 10, data)

			metas, err := s.List()
			if err != nil || len(metas) != 1 || metas[0].ID != id || metas[0].Size != int64(size) || metas[0].Index != 10 {
				t.Fatalf("encrypted=%v size=%d: List = %+v, %v", encrypted, size, metas, err)
			}
			got, err := readFileSnapshot(s, id)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("encrypted=%v size=%d: read %d bytes, err %v", encrypted, size, len(got), err)
			}
			// The image is not held in the database (and so not in its RAM).
			if keys, _ := db.List("raft/snapshot"); len(keys) != 0 {
				t.Fatalf("snapshot records left in CobaltDB: %v", keys)
			}
			if encrypted && size > 64 {
				raw, _ := os.ReadFile(filepath.Join(s.dir, id+snapshotFileSuffix))
				if bytes.Contains(raw, data[:64]) {
					t.Fatal("encrypted snapshot file contains plaintext")
				}
			}
		}
	}
}

// Older versions stored the snapshot inside CobaltDB. It stays readable, and
// the first file snapshot removes it.
func TestFileSnapshotStoreReadsAndRetiresLegacySnapshot(t *testing.T) {
	db := openSnapshotTestDB(t, false)
	legacy := NewCobaltDBSnapshotStore(db)
	sink, _ := legacy.Create(1, 5, 1, nil)
	_, _ = sink.Write([]byte("legacy-image"))
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := NewFileSnapshotStore(db)
	if err != nil {
		t.Fatal(err)
	}
	metas, err := s.List()
	if err != nil || len(metas) != 1 || metas[0].ID != "1-5" {
		t.Fatalf("legacy List = %+v, %v", metas, err)
	}
	if got, err := readFileSnapshot(s, "1-5"); err != nil || string(got) != "legacy-image" {
		t.Fatalf("legacy Open = %q, %v", got, err)
	}

	id := writeFileSnapshot(t, s, 9, []byte("file-image"))
	if keys, _ := db.List("raft/snapshot"); len(keys) != 0 {
		t.Fatalf("legacy records not removed: %v", keys)
	}
	if got, err := readFileSnapshot(s, id); err != nil || string(got) != "file-image" {
		t.Fatalf("file snapshot = %q, %v", got, err)
	}
}

func TestFileSnapshotStoreDetectsDamage(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		data := make([]byte, 2*snapshotFrameData+5)
		_, _ = rand.Read(data)
		setup := func(t *testing.T) (*FileSnapshotStore, string, string) {
			db := openSnapshotTestDB(t, encrypted)
			s, err := NewFileSnapshotStore(db)
			if err != nil {
				t.Fatal(err)
			}
			id := writeFileSnapshot(t, s, 3, data)
			return s, id, filepath.Join(s.dir, id+snapshotFileSuffix)
		}
		frames := func(raw []byte) [][]byte {
			var out [][]byte
			for len(raw) >= snapshotFrameHeader {
				n := int(binary.BigEndian.Uint32(raw[:4])) + snapshotFrameHeader
				out = append(out, raw[:n])
				raw = raw[n:]
			}
			return out
		}
		for name, damage := range map[string]func([]byte) []byte{
			"flipped byte": func(raw []byte) []byte { raw[len(raw)/2] ^= 0xff; return raw },
			"truncated":    func(raw []byte) []byte { return raw[:len(raw)-10] },
			"last frame dropped": func(raw []byte) []byte {
				f := frames(raw)
				return bytes.Join(f[:len(f)-1], nil)
			},
			"frames swapped": func(raw []byte) []byte {
				f := frames(raw)
				f[0], f[1] = f[1], f[0]
				return bytes.Join(f, nil)
			},
		} {
			t.Run(name, func(t *testing.T) {
				s, id, path := setup(t)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, damage(raw), 0o600); err != nil {
					t.Fatal(err)
				}
				if got, err := readFileSnapshot(s, id); err == nil {
					t.Fatalf("encrypted=%v: damaged snapshot read without error (%d bytes)", encrypted, len(got))
				}
			})
		}
	}
}

// A writer that never finishes (crash, Cancel) leaves the previous snapshot
// live; its temporary file is removed by the next completed snapshot.
func TestFileSnapshotStoreInterruptedWriterKeepsPrevious(t *testing.T) {
	db := openSnapshotTestDB(t, false)
	s, err := NewFileSnapshotStore(db)
	if err != nil {
		t.Fatal(err)
	}
	id := writeFileSnapshot(t, s, 1, []byte("first"))

	abandoned, err := s.Create(1, 2, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = abandoned.Write(bytes.Repeat([]byte("x"), snapshotFrameData+1))
	// No Close: as if the process died here.
	if metas, _ := s.List(); len(metas) != 1 || metas[0].ID != id {
		t.Fatalf("an unfinished snapshot replaced the live one: %+v", metas)
	}
	if got, err := readFileSnapshot(s, id); err != nil || string(got) != "first" {
		t.Fatalf("previous snapshot unreadable: %q, %v", got, err)
	}

	cancelled, _ := s.Create(1, 3, 2, nil)
	_, _ = cancelled.Write([]byte("y"))
	if err := cancelled.Cancel(); err != nil {
		t.Fatal(err)
	}

	writeFileSnapshot(t, s, 4, []byte("second"))
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", e.Name())
		}
	}
	if len(entries) != 2 {
		t.Fatalf("snapshot directory holds %d entries, want the snapshot and meta.json", len(entries))
	}
	if _, err := s.Open(id); err == nil {
		t.Fatal("a replaced snapshot can still be opened")
	} else {
		var nf *core.NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("Open(replaced) = %v, want NotFoundError", err)
		}
	}
}
