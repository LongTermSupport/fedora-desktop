package carry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// stagedCtl is where an import stages the archive's ctl/ members (SI8).
func stagedCtl(t paths.TeamID) string { return paths.CarryStaging(t) + "/ctl" }

// StagedLost is the bytes of carry.place's lost exports (SC-C2, sc-D11): each of lost, read
// from the import's staged ctl/ in lost's order, to follow the request on the child's stdin.
// Every check is done before it is returned: each name a lost export's plain name, once; the
// staged ctl/ holding no lost export lost does not name; each file opened with no link followed,
// regular, at most LostCap, and of its size and sha256. The stream digests each file again as it
// reads it and fails, rather than ending, when one is not what was checked.
func StagedLost(f Files, team paths.TeamID, lost []LostExport) (io.Reader, error) {
	dir := stagedCtl(team)
	named := make(map[string]bool, len(lost))
	for _, l := range lost {
		if err := l.Check(); err != nil {
			return nil, fmt.Errorf("carry.place's lost exports: %w", err)
		}
		if named[l.Name] {
			return nil, fmt.Errorf("carry.place's lost exports name %s twice", l.Name)
		}
		named[l.Name] = true
	}
	entries, err := f.List(dir)
	if err != nil {
		return nil, fmt.Errorf("the staged %s: %w", dir, err)
	}
	for _, e := range entries {
		if lostName.MatchString(e.Name) && !named[e.Name] {
			return nil, fmt.Errorf("the staged %s/%s is a lost export carry.place's request does not name", dir, e.Name)
		}
	}
	for _, l := range lost {
		r, err := openLost(f, dir, l)
		if err != nil {
			return nil, err
		}
		if err = r.drain(); err != nil {
			return nil, err
		}
	}
	return &lostStream{f: f, dir: dir, lost: lost}, nil
}

// lostFile is one lost export being read: its bytes up to its size, digested as they pass.
type lostFile struct {
	l    LostExport
	path string
	file io.ReadCloser
	body io.Reader
	sum  hash.Hash
	n    int64
}

// openLost opens l in dir with no link followed: a regular file of l's size, at most LostCap.
func openLost(f Files, dir string, l LostExport) (*lostFile, error) {
	path := dir + "/" + l.Name
	file, info, err := f.Open(path)
	if err != nil {
		return nil, fmt.Errorf("the staged lost export: %w", err)
	}
	if info.Size > LostCap || info.Size != l.Size {
		return nil, errors.Join(fmt.Errorf("the staged %s is %d bytes, not %d (at most %d)", path, info.Size, l.Size, LostCap), file.Close())
	}
	sum := sha256.New()
	return &lostFile{l: l, path: path, file: file, body: io.TeeReader(io.LimitReader(file, l.Size), sum), sum: sum}, nil
}

func (r *lostFile) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	r.n += int64(n)
	return n, err
}

// end closes the file, which must have given exactly its size of bytes of its sha256.
func (r *lostFile) end() error {
	err := r.file.Close()
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", r.path, err)
	}
	if got := hex.EncodeToString(r.sum.Sum(nil)); r.n != r.l.Size || got != r.l.SHA256 {
		return fmt.Errorf("the staged %s is not the %d bytes of sha256 %s carry.place names", r.path, r.l.Size, r.l.SHA256)
	}
	return nil
}

// drain reads the file to its end and checks it.
func (r *lostFile) drain() error {
	_, err := io.Copy(io.Discard, r)
	if err != nil {
		return errors.Join(fmt.Errorf("cannot read %s: %w", r.path, err), r.file.Close())
	}
	return r.end()
}

// lostStream is StagedLost's reader: each file opened as the one before it ends, and checked at
// its own end.
type lostStream struct {
	f    Files
	dir  string
	lost []LostExport
	next int
	cur  *lostFile
	err  error
}

func (s *lostStream) Read(p []byte) (int, error) {
	for s.err == nil {
		if s.cur == nil {
			if s.next == len(s.lost) {
				return 0, io.EOF
			}
			s.cur, s.err = openLost(s.f, s.dir, s.lost[s.next])
			s.next++
			continue
		}
		n, err := s.cur.Read(p)
		switch {
		case errors.Is(err, io.EOF):
			s.err, s.cur = s.cur.end(), nil
		case err != nil:
			s.err = errors.Join(fmt.Errorf("cannot read %s: %w", s.cur.path, err), s.cur.file.Close())
		}
		if n > 0 || s.err != nil {
			return n, s.err
		}
	}
	return 0, s.err
}
