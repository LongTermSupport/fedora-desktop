package host

import (
	"os"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// RU1: the staging unit runs the version units.json records, not the pin.
func TestReadUnitsVersionIsTheRecordedOne(t *testing.T) {
	w := applied(t)
	w.version = "1.9.5"
	got, err := ReadUnitsVersion(w.root, ops(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != pin(t).Version {
		t.Fatalf("ReadUnitsVersion = %s, want %s", got, pin(t).Version)
	}
}

// RU1: no units.json, or one that does not decode, is an error.
func TestReadUnitsVersionNeedsAGoodUnitsJSON(t *testing.T) {
	w := applied(t)
	w.write(paths.UnitsRecord(ops(t)), "{}")
	if _, err := ReadUnitsVersion(w.root, ops(t)); err == nil {
		t.Fatal("a bad units.json was read")
	}
	if err := os.Remove(w.at(paths.UnitsRecord(ops(t)))); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadUnitsVersion(w.root, ops(t)); err == nil || !strings.Contains(err.Error(), "never applied") {
		t.Fatalf("no units.json = %v", err)
	}
}

// RS7: the installed config's registration key is the one the apply made.
func TestRegKeyOfTheInstalledConfig(t *testing.T) {
	w := applied(t)
	key := RegKeyOf([]byte(w.read(paths.ServerConfig(ops(t)))))
	if !regKeyGrammar.MatchString(key) {
		t.Fatalf("RegKeyOf = %q", key)
	}
}
