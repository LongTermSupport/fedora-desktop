package carry

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// lostWorld is an import's staged ctl/ directory with lost exports in it.
type lostWorld struct {
	t    *testing.T
	root string
	team paths.TeamID
}

func newLostWorld(t *testing.T) *lostWorld {
	t.Helper()
	w := &lostWorld{t: t, root: t.TempDir(), team: sampleTeam(t)}
	if err := os.MkdirAll(w.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *lostWorld) dir() string { return w.root + paths.CarryStaging(w.team) + "/ctl" }

// put writes a staged file and answers the export the request names for it.
func (w *lostWorld) put(name, data string) LostExport {
	w.t.Helper()
	if err := os.WriteFile(filepath.Join(w.dir(), name), []byte(data), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return lostOf(name, data)
}

func lostOf(name, data string) LostExport {
	sum := sha256.Sum256([]byte(data))
	return LostExport{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func (w *lostWorld) stream(lost []LostExport) (io.Reader, error) {
	return StagedLost(OS{Root: w.root}, w.team, lost)
}

// TestStagedLostStreamsEachNamedFileInOrder is SC-C2 as ruled (sc-D11): root streams each lost
// export the request names from CarryStaging(t), in the request's order, nothing else.
func TestStagedLostStreamsEachNamedFileInOrder(t *testing.T) {
	w := newLostWorld(t)
	two := w.put("restore-2-lost.jsonl", "{\"b\":2}\n")
	one := w.put("restore-1-lost.jsonl", "{\"a\":1}\n")
	w.put("roster.json", "{}")
	r, err := w.stream([]LostExport{two, one})
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "{\"b\":2}\n{\"a\":1}\n" {
		t.Fatalf("%q %v", got, err)
	}
	w = newLostWorld(t)
	w.put("roster.json", "{}")
	r, err = w.stream(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(r); err != nil || len(got) != 0 {
		t.Fatalf("no lost export streamed %q %v", got, err)
	}
}

// TestStagedLostRefusals is the ruling's list: each refused before a byte is streamed.
func TestStagedLostRefusals(t *testing.T) {
	good := "{\"a\":1}\n"
	for name, c := range map[string]func(w *lostWorld) []LostExport{
		"traversal": func(w *lostWorld) []LostExport {
			w.put("restore-1-lost.jsonl", good)
			return []LostExport{lostOf("../restore-1-lost.jsonl", good)}
		},
		"a path": func(w *lostWorld) []LostExport {
			w.put("restore-1-lost.jsonl", good)
			return []LostExport{lostOf("ctl/restore-1-lost.jsonl", good)}
		},
		"not a lost export's name": func(w *lostWorld) []LostExport {
			return []LostExport{w.put("roster.json", good)}
		},
		"a symlink": func(w *lostWorld) []LostExport {
			w.put("other", good)
			if err := os.Symlink("other", filepath.Join(w.dir(), "restore-1-lost.jsonl")); err != nil {
				t.Fatal(err)
			}
			return []LostExport{lostOf("restore-1-lost.jsonl", good)}
		},
		"oversize": func(w *lostWorld) []LostExport {
			path := filepath.Join(w.dir(), "restore-1-lost.jsonl")
			w.put("restore-1-lost.jsonl", "")
			if err := os.Truncate(path, LostCap+1); err != nil {
				t.Fatal(err)
			}
			return []LostExport{{Name: "restore-1-lost.jsonl", Size: LostCap + 1, SHA256: strings.Repeat("0", 64)}}
		},
		"a digest mismatch": func(w *lostWorld) []LostExport {
			w.put("restore-1-lost.jsonl", good)
			return []LostExport{lostOf("restore-1-lost.jsonl", "{\"a\":2}\n")}
		},
		"a size mismatch": func(w *lostWorld) []LostExport {
			w.put("restore-1-lost.jsonl", good)
			return []LostExport{lostOf("restore-1-lost.jsonl", good+"x")}
		},
		"a staged name not in the request": func(w *lostWorld) []LostExport {
			w.put("restore-2-lost.jsonl", good)
			return []LostExport{w.put("restore-1-lost.jsonl", good)}
		},
		"a name absent": func(*lostWorld) []LostExport {
			return []LostExport{lostOf("restore-1-lost.jsonl", good)}
		},
		"a name twice": func(w *lostWorld) []LostExport {
			l := w.put("restore-1-lost.jsonl", good)
			return []LostExport{l, l}
		},
		"a bad digest": func(w *lostWorld) []LostExport {
			l := w.put("restore-1-lost.jsonl", good)
			l.SHA256 = strings.ToUpper(l.SHA256)
			return []LostExport{l}
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newLostWorld(t)
			lost := c(w)
			if r, err := w.stream(lost); err == nil || r != nil {
				t.Fatalf("streamed: %v", err)
			}
		})
	}
}

// TestStagedLostFailsAFileChangedAfterItsCheck: the stream digests each file again and fails
// at its end when it is not what was checked, so the child sees a short or wrong stream.
func TestStagedLostFailsAFileChangedAfterItsCheck(t *testing.T) {
	w := newLostWorld(t)
	l := w.put("restore-1-lost.jsonl", "{\"a\":1}\n")
	r, err := w.stream([]LostExport{l})
	if err != nil {
		t.Fatal(err)
	}
	w.put("restore-1-lost.jsonl", "{\"a\":2}\n")
	_, err = io.ReadAll(r)
	if err == nil {
		t.Fatal("a changed file was streamed whole")
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("the failure is an end of stream")
	}
}
