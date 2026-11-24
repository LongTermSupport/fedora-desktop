package v1

import (
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// E18: the newest record is the newest stamp; a temporary directory or another name is no record.
func TestNewestIgnoresOtherNames(t *testing.T) {
	w := newWorld(t)
	base := paths.AdminStateRoot + "/_v1-export/alpha"
	for _, d := range []string{"/20250101T000000Z", "/20260101T000000Z", "/.20270101T000000Z.part-1", "/zzz"} {
		w.mkdir(base+d, 0o700)
	}
	w.write(base+"/20280101T000000Z", "", 0o600)
	got, err := newest(w.root + base)
	if err != nil || got != "20260101T000000Z" {
		t.Fatalf("%q %v", got, err)
	}
	w.mkdir("/empty", 0o700)
	if _, err := newest(w.root + "/empty"); err == nil || !strings.Contains(err.Error(), "no record") {
		t.Fatalf("%v", err)
	}
}

// E16: the digest counts each event's type, and an event with no readable type under its own key.
func TestTypeCounts(t *testing.T) {
	got := typeCounts([][]byte{[]byte(`{"type":"a"}`), []byte(`{"type":"a"}`), []byte(`{"type":1}`), []byte(`{}`)})
	if len(got) != 2 || got["a"] != 2 || got[noType] != 2 {
		t.Fatalf("%v", got)
	}
}
