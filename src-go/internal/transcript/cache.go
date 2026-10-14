package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

const (
	// maxLine bounds the memory a line that is still being written can hold. Claude's longest
	// lines are a tool result or an image; a line past this is an error rather than an unbounded
	// buffer.
	maxLine = 64 << 20
	// maxFiles bounds how many transcripts a Cache remembers; the least recently asked about
	// is forgotten first.
	maxFiles = 64
	// chunkSize is how much of a file is read at a time.
	chunkSize = 1 << 20
	// headLen is how many leading bytes identify a file that was rewritten in place.
	headLen = 256
)

// Cache answers the questions this package answers of a transcript without reading the
// transcript whole each time. A transcript is only ever appended to, so for each file it keeps
// what it has derived from the bytes read so far and, on the next question, reads only the
// bytes added since. A file that shrank, was replaced (another device or inode) or was
// rewritten in place (its first bytes changed) is read again from the start. A rewrite that
// keeps the length, the inode and the first bytes is not seen.
//
// The memory held per file is a fixed-size summary plus the unterminated last line, and the
// number of files is bounded by maxFiles. A Cache is safe for concurrent use.
type Cache struct {
	mu    sync.Mutex
	files map[string]*fileState
	tick  uint64
	// bytesRead is how many bytes of transcripts this Cache has read, for the tests.
	bytesRead int64
}

// NewCache is an empty Cache. The package-level ContextTokens, TookInputSince and
// CompactedSince use a new one per call, which is a full read.
func NewCache() *Cache {
	return &Cache{files: map[string]*fileState{}}
}

// fileID is what tells one file from another that replaced it at the same path. The device and
// inode are the integer types the platform's stat gives, compared as they are.
type fileID struct{ dev, ino any }

// fileState is what is known of one transcript.
type fileState struct {
	id fileID
	// head is the first bytes of the file, up to headLen, as they were first read.
	head []byte
	// offset is how many bytes have been read, the unterminated last line included.
	offset int64
	// partial is the bytes after the last newline.
	partial []byte
	sum     summary
	used    uint64
}

// summary is everything a question needs from the lines read so far.
type summary struct {
	last       float64
	found      bool
	contextErr error
	input      stamps
	compaction stamps
}

// stamps is the latest dated entry of one kind, or why one could not be dated. The first
// failure is kept, as a full read stops at the first.
type stamps struct {
	latest int64
	any    bool
	err    error
}

func (s *stamps) add(e entry) {
	if s.err != nil || !e.Timestamp.Present {
		return
	}
	at, err := epoch(e.Timestamp.Value)
	if err != nil {
		s.err = err
		return
	}
	if !s.any || at > s.latest {
		s.latest, s.any = at, true
	}
}

func (s *stamps) since(path string, since int64) (bool, error) {
	if s.err != nil {
		return false, fmt.Errorf("the transcript %s could not be read: %w", path, s.err)
	}
	return s.any && s.latest >= since, nil
}

// addLine folds in one line; a line that is not a JSON object counts for nothing.
func (s *summary) addLine(line []byte) {
	var e entry
	if json.Unmarshal(line, &e) != nil || !mainThread(e) {
		return
	}
	if s.contextErr == nil {
		size, ok, err := contextOf(e)
		switch {
		case err != nil:
			s.contextErr = err
		case ok:
			s.last, s.found = size, true
		}
	}
	if e.Type.Value == typeUser && !bool(e.IsMeta) {
		s.input.add(e)
	}
	if e.Type.Value == typeSystem && e.Subtype.Value == subtypeBoundary {
		s.compaction.add(e)
	}
}

var errLineTooLong = fmt.Errorf("a line longer than %d bytes", maxLine)

// consume folds in the next bytes of the file, which begin at the state's offset.
func (st *fileState) consume(chunk []byte) error {
	if len(st.head) < headLen && int64(len(st.head)) == st.offset {
		st.head = append(st.head, chunk[:min(headLen-len(st.head), len(chunk))]...)
	}
	st.offset += int64(len(chunk))
	for len(chunk) > 0 {
		cut := bytes.IndexByte(chunk, '\n')
		if cut < 0 {
			st.partial = append(st.partial, chunk...)
			break
		}
		line := chunk[:cut]
		if len(st.partial) > 0 {
			st.partial = append(st.partial, line...)
			line = st.partial
		}
		if len(line) > maxLine {
			return errLineTooLong
		}
		st.sum.addLine(line)
		if cap(st.partial) > chunkSize {
			st.partial = nil
		} else {
			st.partial = st.partial[:0]
		}
		chunk = chunk[cut+1:]
	}
	if len(st.partial) > maxLine {
		return errLineTooLong
	}
	return nil
}

// ContextTokens is the package function of that name, answered from what the Cache already
// holds of the file.
func (c *Cache) ContextTokens(path string) (int64, error) {
	s, err := c.view(path)
	if err != nil {
		return 0, err
	}
	if s.contextErr != nil {
		return 0, fmt.Errorf("the transcript %s could not be read: %w", path, s.contextErr)
	}
	if !s.found || s.last < 0 || s.last != float64(int64(s.last)) {
		return 0, fmt.Errorf("the transcript %s records no context size", path)
	}
	return int64(s.last), nil
}

// TookInputSince is the package function of that name, answered from what the Cache already
// holds of the file.
func (c *Cache) TookInputSince(path string, since int64) (bool, error) {
	s, err := c.view(path)
	if err != nil {
		return false, err
	}
	return s.input.since(path, since)
}

// CompactedSince is the package function of that name, answered from what the Cache already
// holds of the file.
func (c *Cache) CompactedSince(path string, since int64) (bool, error) {
	s, err := c.view(path)
	if err != nil {
		return false, err
	}
	return s.compaction.since(path, since)
}

// view is the summary of the file as it is now, brought up to date by reading what was added.
// The unterminated last line counts if it is whole JSON, as it does in a full read, without
// being committed: more of it may still be written.
func (c *Cache) view(path string) (summary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := c.refresh(path)
	if err != nil {
		delete(c.files, path)
		return summary{}, fmt.Errorf("the transcript %s could not be read: %w", path, err)
	}
	s := st.sum
	if len(st.partial) > 0 {
		s.addLine(st.partial)
	}
	return s, nil
}

// refresh brings the state of path up to date and returns it; the caller holds c.mu.
func (c *Cache) refresh(path string) (*fileState, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	st, err := c.refreshOpen(path, f)
	return st, errors.Join(err, f.Close())
}

func (c *Cache) refreshOpen(path string, f *os.File) (*fileState, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errors.New("the file cannot be told from another")
	}
	id := fileID{dev: raw.Dev, ino: raw.Ino}
	st := c.files[path]
	if st != nil && (st.id != id || info.Size() < st.offset || !sameHead(f, st.head)) {
		st = nil
	}
	if st == nil {
		st = &fileState{id: id}
		c.remember(path, st)
	}
	c.tick++
	st.used = c.tick
	if info.Size() == st.offset {
		return st, nil
	}
	return st, c.readNew(f, st, info.Size()-st.offset)
}

// sameHead says whether the file still begins with the bytes first read from it.
func sameHead(f *os.File, head []byte) bool {
	if len(head) == 0 {
		return true
	}
	got := make([]byte, len(head))
	n, err := f.ReadAt(got, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	return bytes.Equal(got[:n], head)
}

// readNew reads from the state's offset to the end of the file, which was missing bytes long
// when it was measured; a file that grew since is read to its new end.
func (c *Cache) readNew(f *os.File, st *fileState, missing int64) error {
	buf := make([]byte, min(missing, chunkSize))
	for {
		n, err := f.ReadAt(buf, st.offset)
		c.bytesRead += int64(n)
		if n > 0 {
			if consumeErr := st.consume(buf[:n]); consumeErr != nil {
				return consumeErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
	}
}

// remember holds st for path, forgetting the least recently used file beyond maxFiles.
func (c *Cache) remember(path string, st *fileState) {
	if _, held := c.files[path]; !held && len(c.files) >= maxFiles {
		oldest, oldestUsed := "", ^uint64(0)
		for p, s := range c.files {
			if s.used < oldestUsed {
				oldest, oldestUsed = p, s.used
			}
		}
		delete(c.files, oldest)
	}
	c.files[path] = st
}
