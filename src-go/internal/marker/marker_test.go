package marker

import (
	"slices"
	"strings"
	"testing"
)

func TestFieldsKeepsOnlyTheWantedMarker(t *testing.T) {
	out := "note\nSEAT\ta\tb\r\nSEATX\tz\nSEAT\tc\td\nCHANGED\tx\n"
	rows, err := Fields(out, "SEAT", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"SEAT", "a", "b"}, {"SEAT", "c", "d"}}
	if len(rows) != 2 || !slices.Equal(rows[0], want[0]) || !slices.Equal(rows[1], want[1]) {
		t.Fatalf("got %q", rows)
	}
}

func TestFieldsWithWrongCountFails(t *testing.T) {
	_, err := Fields("SEAT\ta\n", "SEAT", 3)
	if err == nil || !strings.Contains(err.Error(), "SEAT line has 2 fields, want 3") {
		t.Fatalf("got %v", err)
	}
}

func TestFieldsBareMarkerCountsAsOneField(t *testing.T) {
	if _, err := Fields("SEAT\n", "SEAT", 2); err == nil {
		t.Fatal("accepted a bare marker")
	}
	if rows, err := Fields("", "SEAT", 2); err != nil || rows != nil {
		t.Fatalf("empty output: %v %v", rows, err)
	}
}

func TestDashEmpty(t *testing.T) {
	if DashEmpty("-") != "" || DashEmpty("x") != "x" || DashEmpty("") != "" {
		t.Fatal("DashEmpty")
	}
}
