package storage

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
)

// FileSnapshotStore keeps Raft snapshots as files under <data>/raft-snapshots
// instead of inside CobaltDB, whose in-memory B+Tree and WAL would otherwise
// hold a second full copy of the state machine.
//
// A snapshot is a sequence of frames, each [len uint32][crc32 uint32][payload].
// The payload is [index uint64][final byte][data], encrypted with the
// database's key when storage encryption is on, so frames cannot be reordered,
// dropped or truncated unnoticed. meta.json names the one live snapshot; it is
// replaced atomically after the snapshot file is complete and synced, so an
// interrupted writer never replaces the previous snapshot.
//
// Snapshots written by older versions inside CobaltDB stay readable through
// the legacy store until the first file snapshot replaces them.
type FileSnapshotStore struct {
	db     *CobaltDB
	dir    string
	legacy *CobaltDBSnapshotStore
	mu     sync.Mutex
}

const (
	snapshotDirName     = "raft-snapshots"
	snapshotMetaFile    = "meta.json"
	snapshotFileSuffix  = ".snap"
	snapshotFrameHeader = 8                 // len + crc32
	snapshotMaxFrame    = 16 << 20          // bounds a corrupt length prefix
	snapshotPayloadHead = 9                 // chunk index + final flag
	snapshotFrameData   = snapshotChunkSize // plaintext bytes per frame
)

type fileSnapshotMeta struct {
	raft.SnapshotMeta
	File   string `json:"file"`
	Frames int    `json:"frames"`
}

// NewFileSnapshotStore creates the snapshot directory inside db's data
// directory.
func NewFileSnapshotStore(db *CobaltDB) (*FileSnapshotStore, error) {
	dir := filepath.Join(db.path, snapshotDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create snapshot directory: %w", err)
	}
	return &FileSnapshotStore{db: db, dir: dir, legacy: NewCobaltDBSnapshotStore(db)}, nil
}

func (s *FileSnapshotStore) readMeta() (*fileSnapshotMeta, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, snapshotMetaFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var meta fileSnapshotMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("read snapshot metadata: %w", err)
	}
	if meta.File == "" || meta.File != filepath.Base(meta.File) || !strings.HasSuffix(meta.File, snapshotFileSuffix) {
		return nil, fmt.Errorf("invalid snapshot file name %q", meta.File)
	}
	return &meta, nil
}

// List returns the live snapshot, or the legacy in-database one.
func (s *FileSnapshotStore) List() ([]raft.SnapshotMeta, error) {
	s.mu.Lock()
	meta, err := s.readMeta()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return s.legacy.List()
	}
	return []raft.SnapshotMeta{meta.SnapshotMeta}, nil
}

// Open streams the snapshot with the given ID.
func (s *FileSnapshotStore) Open(id string) (raft.SnapshotSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, err := s.readMeta()
	if err != nil {
		return nil, err
	}
	if meta == nil {
		return s.legacy.Open(id)
	}
	if meta.ID != id {
		return nil, &core.NotFoundError{Entity: "snapshot", ID: id}
	}
	f, err := os.Open(filepath.Join(s.dir, meta.File))
	if err != nil {
		return nil, fmt.Errorf("open snapshot: %w", err)
	}
	return &fileSnapshotSource{store: s, file: f, r: bufio.NewReader(f), meta: meta}, nil
}

// Create starts writing a snapshot to a temporary file.
func (s *FileSnapshotStore) Create(version, index, term uint64, configuration []byte) (raft.SnapshotSink, error) {
	tmp, err := os.CreateTemp(s.dir, "snapshot-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create snapshot file: %w", err)
	}
	return &fileSnapshotSink{
		store: s, file: tmp, w: bufio.NewWriterSize(tmp, 1<<20),
		meta: fileSnapshotMeta{SnapshotMeta: raft.SnapshotMeta{
			ID: fmt.Sprintf("%d-%d", term, index), Index: index, Term: term, Version: version,
		}},
		buf: make([]byte, 0, snapshotFrameData),
	}, nil
}

func (s *FileSnapshotStore) seal(payload []byte) ([]byte, error) {
	if s.db.encryptor == nil {
		return payload, nil
	}
	return s.db.encryptor.encrypt(payload)
}

func (s *FileSnapshotStore) open(payload []byte) ([]byte, error) {
	if s.db.encryptor == nil {
		return payload, nil
	}
	return s.db.encryptor.decrypt(payload)
}

type fileSnapshotSink struct {
	store  *FileSnapshotStore
	file   *os.File
	w      *bufio.Writer
	meta   fileSnapshotMeta
	buf    []byte
	closed bool
	err    error
}

func (k *fileSnapshotSink) ID() string { return k.meta.ID }

func (k *fileSnapshotSink) Write(p []byte) (int, error) {
	if k.closed {
		return 0, errors.New("sink is closed")
	}
	if k.err != nil {
		return 0, k.err
	}
	written := 0
	for len(p) > 0 {
		n := min(len(p), snapshotFrameData-len(k.buf))
		k.buf = append(k.buf, p[:n]...)
		p = p[n:]
		written += n
		if len(k.buf) == snapshotFrameData {
			if err := k.flush(false); err != nil {
				k.err = err
				return written, err
			}
		}
	}
	return written, nil
}

// flush writes the buffered data as one frame. The last frame carries the
// final flag (it may be empty), so a truncated file is detected.
func (k *fileSnapshotSink) flush(final bool) error {
	payload := make([]byte, snapshotPayloadHead+len(k.buf))
	binary.BigEndian.PutUint64(payload, uint64(k.meta.Frames))
	if final {
		payload[8] = 1
	}
	copy(payload[snapshotPayloadHead:], k.buf)
	sealed, err := k.store.seal(payload)
	if err != nil {
		return err
	}
	var head [snapshotFrameHeader]byte
	binary.BigEndian.PutUint32(head[:4], uint32(len(sealed))) // #nosec G115 -- frames are bounded by snapshotMaxFrame
	binary.BigEndian.PutUint32(head[4:], crc32.ChecksumIEEE(sealed))
	if _, err := k.w.Write(head[:]); err != nil {
		return err
	}
	if _, err := k.w.Write(sealed); err != nil {
		return err
	}
	k.meta.Size += int64(len(k.buf))
	k.meta.Frames++
	k.buf = k.buf[:0]
	return nil
}

func (k *fileSnapshotSink) abort(err error) error {
	k.err = err
	_ = k.file.Close()
	_ = os.Remove(k.file.Name())
	return err
}

// Close completes the snapshot: the file is synced and renamed, then
// meta.json is replaced, then older snapshots are removed.
func (k *fileSnapshotSink) Close() error {
	if k.closed {
		return k.err
	}
	k.closed = true
	if k.err != nil {
		return k.abort(k.err)
	}
	if err := k.flush(true); err != nil {
		return k.abort(err)
	}
	if err := k.w.Flush(); err != nil {
		return k.abort(err)
	}
	if err := k.file.Sync(); err != nil {
		return k.abort(err)
	}
	if err := k.file.Close(); err != nil {
		return k.abort(err)
	}

	s := k.store
	s.mu.Lock()
	defer s.mu.Unlock()
	k.meta.File = k.meta.ID + snapshotFileSuffix
	final := filepath.Join(s.dir, k.meta.File)
	if err := os.Rename(k.file.Name(), final); err != nil {
		_ = os.Remove(k.file.Name())
		k.err = err
		return err
	}
	data, err := json.Marshal(k.meta)
	if err != nil {
		k.err = err
		return err
	}
	if err := writeFileAtomic(filepath.Join(s.dir, snapshotMetaFile), data); err != nil {
		k.err = err
		return err
	}

	// The new snapshot is live. Cleanup failures only leave unreachable files.
	if entries, err := os.ReadDir(s.dir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if name != k.meta.File && name != snapshotMetaFile &&
				(strings.HasSuffix(name, snapshotFileSuffix) || strings.HasSuffix(name, ".tmp")) {
				_ = os.Remove(filepath.Join(s.dir, name))
			}
		}
	}
	s.dropLegacy()
	return nil
}

func (k *fileSnapshotSink) Cancel() error {
	if k.closed {
		return nil
	}
	k.closed = true
	_ = k.file.Close()
	return os.Remove(k.file.Name())
}

// dropLegacy removes a snapshot stored inside CobaltDB by an older version.
func (s *FileSnapshotStore) dropLegacy() {
	s.db.snapshotMu.Lock()
	defer s.db.snapshotMu.Unlock()
	if _, err := s.db.Get("raft/snapshot-meta"); err != nil {
		return
	}
	_ = s.db.DeletePrefix("raft/snapshot-data/")
	_ = s.db.Delete("raft/snapshot")
	_ = s.db.Delete("raft/snapshot-meta")
}

// writeFileAtomic replaces path with data: temp file, fsync, rename, then a
// directory fsync so the rename survives a power loss.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return syncDir(filepath.Dir(path))
}

type fileSnapshotSource struct {
	store   *FileSnapshotStore
	file    *os.File
	r       *bufio.Reader
	meta    *fileSnapshotMeta
	pending []byte
	frame   int
	read    int64
	done    bool
}

func (src *fileSnapshotSource) Read(p []byte) (int, error) {
	for len(src.pending) == 0 {
		if src.done {
			if src.read != src.meta.Size || src.frame != src.meta.Frames {
				return 0, fmt.Errorf("snapshot %s is incomplete", src.meta.ID)
			}
			return 0, io.EOF
		}
		if err := src.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, src.pending)
	src.pending = src.pending[n:]
	return n, nil
}

func (src *fileSnapshotSource) next() error {
	var head [snapshotFrameHeader]byte
	if _, err := io.ReadFull(src.r, head[:]); err != nil {
		return fmt.Errorf("snapshot %s is truncated: %w", src.meta.ID, err)
	}
	size := binary.BigEndian.Uint32(head[:4])
	if size > snapshotMaxFrame {
		return fmt.Errorf("snapshot %s has a corrupt frame", src.meta.ID)
	}
	sealed := make([]byte, size)
	if _, err := io.ReadFull(src.r, sealed); err != nil {
		return fmt.Errorf("snapshot %s is truncated: %w", src.meta.ID, err)
	}
	if crc32.ChecksumIEEE(sealed) != binary.BigEndian.Uint32(head[4:]) {
		return fmt.Errorf("snapshot %s has a corrupt frame", src.meta.ID)
	}
	payload, err := src.store.open(sealed)
	if err != nil {
		return fmt.Errorf("snapshot %s frame %d: %w", src.meta.ID, src.frame, err)
	}
	if len(payload) < snapshotPayloadHead || binary.BigEndian.Uint64(payload) != uint64(src.frame) {
		return fmt.Errorf("snapshot %s frame %d is out of order", src.meta.ID, src.frame)
	}
	src.frame++
	src.done = payload[8] == 1
	src.pending = payload[snapshotPayloadHead:]
	src.read += int64(len(src.pending))
	return nil
}

func (src *fileSnapshotSource) Close() error { return src.file.Close() }
