package tokens

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// today is the fixed day the cases count from.
var today = time.Date(2026, time.October, 21, 15, 4, 5, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func mustParse(t *testing.T, file string) File {
	t.Helper()
	f, ok := ParseFileName(file)
	if !ok {
		t.Fatalf("ParseFileName(%q) refused", file)
	}
	return f
}

func TestParseFileName(t *testing.T) {
	cases := []struct {
		name, file string
		want       File
		ok         bool
	}{
		{"a dated file", "work.2030-06-01.token", File{Name: "work", Expiry: day(2030, time.June, 1), Dated: true}, true},
		{"a name with a dot", "work.extra.2030-06-01.token", File{Name: "work.extra", Expiry: day(2030, time.June, 1), Dated: true}, true},
		{"an undated file keeps its file name", "undated.token", File{Name: "undated"}, true},
		{"a backup stamp is not a date", "work.backup.token", File{Name: "work.backup"}, true},
		// bash matched the date anywhere before .token, so this read as dated.
		{"a date with no dot before it is not a dated file", "work2030-06-01.token", File{Name: "work2030-06-01"}, true},
		// bash compared the text, so this read as a valid date in the far future.
		{"a date that is not a calendar day is not a dated file", "work.2030-13-45.token", File{Name: "work.2030-13-45"}, true},
		{"not a token file", "work.2030-06-01.token.bak", File{}, false},
		{"a date with no name", ".2030-06-01.token", File{}, false},
		{"nothing before .token", ".token", File{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseFileName(c.file)
			if ok != c.ok {
				t.Fatalf("ParseFileName(%q) ok = %v, want %v", c.file, ok, c.ok)
			}
			c.want.FileName = c.file
			if ok && got != c.want {
				t.Fatalf("ParseFileName(%q) = %+v, want %+v", c.file, got, c.want)
			}
		})
	}
}

func TestFileNameRoundTrips(t *testing.T) {
	name, err := FileName("work", day(2031, time.January, 9))
	if err != nil || name != "work.2031-01-09.token" {
		t.Fatalf("FileName = %q, %v", name, err)
	}
	if _, err := FileName("bad.name", day(2031, time.January, 9)); !errors.Is(err, ErrBadName) {
		t.Fatalf("FileName of a bad name = %v, want ErrBadName", err)
	}
}

func TestValidName(t *testing.T) {
	for _, good := range []string{"work", "Work_2", "a-b", "x"} {
		if err := ValidName(good); err != nil {
			t.Errorf("ValidName(%q) = %v", good, err)
		}
	}
	// The import-cleanup suite's case: a name outside letters, digits, _ and - is refused.
	for _, bad := range []string{"bad.name", "", "a b", "a/b", "émile", "a'b"} {
		if err := ValidName(bad); !errors.Is(err, ErrBadName) {
			t.Errorf("ValidName(%q) = %v, want ErrBadName", bad, err)
		}
	}
}

// The expiry-stamp boundary cases of scripts/test-ccy-token-mode.bash.
func TestStatusAndIsValid(t *testing.T) {
	cases := []struct {
		name  string
		file  string
		want  Status
		valid bool
	}{
		{"yesterday-stamped token is expired", "stale.2026-10-20.token", StatusExpired, false},
		{"today-stamped token is expired (expiring today = expired)", "today.2026-10-21.token", StatusExpiresToday, false},
		{"future-stamped token is valid", "fresh.2027-10-21.token", StatusValid, true},
		{"tomorrow is valid", "fresh.2026-10-22.token", StatusValid, true},
		{"an old-format token without an expiry date is not valid", "old.token", StatusUndated, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := mustParse(t, c.file)
			if got := f.Status(today); got != c.want {
				t.Fatalf("Status = %v, want %v", got, c.want)
			}
			if got := IsValid(c.file, today); got != c.valid {
				t.Fatalf("IsValid = %v, want %v", got, c.valid)
			}
		})
	}
	if IsValid("not-a-token", today) {
		t.Fatal("a file that is not a token file is valid")
	}
}

func TestDaysLeftIgnoresTheTimeOfDayAndTheZone(t *testing.T) {
	f := mustParse(t, "x.2026-10-22.token")
	late := time.Date(2026, time.October, 21, 23, 59, 0, 0, time.FixedZone("east", 14*3600))
	if got := f.DaysLeft(late); got != 1 {
		t.Fatalf("DaysLeft = %d, want 1", got)
	}
}

func TestStatusStrings(t *testing.T) {
	want := map[Status]string{
		StatusValid: "✓ Valid", StatusExpiresToday: "⚠ Expires TODAY", StatusExpired: "✗ EXPIRED",
		StatusUndated: "✗ INVALID FORMAT (missing expiry date)",
	}
	for s, w := range want {
		if s.String() != w {
			t.Errorf("%d.String() = %q, want %q", s, s.String(), w)
		}
	}
}

// The expiry colour cases of scripts/test-ccy-token-mode.bash.
func TestColourExpiry(t *testing.T) {
	const red, yellow, green, reset = "\x1b[31m", "\x1b[33m", "\x1b[32m", "\x1b[0m"
	cases := []struct {
		name, date, want string
	}{
		{"expired is red", "2026-10-20", red + "2026-10-20" + reset},
		{"expiring today is red (already refused)", "2026-10-21", red + "2026-10-21" + reset},
		{"14 days left is yellow", "2026-11-04", yellow + "2026-11-04" + reset},
		{"15 days left is green", "2026-11-05", green + "2026-11-05" + reset},
		{"an unparseable date is printed plain", "not-a-date", "not-a-date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ColourExpiry(c.date, today, true); got != c.want {
				t.Fatalf("ColourExpiry(%q) = %q, want %q", c.date, got, c.want)
			}
		})
	}
	// bash coloured whatever it wrote to; colour here follows the terminal.
	if got := ColourExpiry("2026-10-20", today, false); got != "2026-10-20" {
		t.Fatalf("ColourExpiry without colour = %q", got)
	}
}

func TestExpiryLabel(t *testing.T) {
	if got := ExpiryLabel("fresh.2027-10-21.token", today, true); got != " (expires: \x1b[32m2027-10-21\x1b[0m)" {
		t.Fatalf("ExpiryLabel = %q", got)
	}
	if got := ExpiryLabel("undated.token", today, true); got != "" {
		t.Fatalf("an undated file gets no label, got %q", got)
	}
}

// The selection rule of token_shadow_warning, and of the import's "other files": only
// <name>.<YYYY-MM-DD>.token files other than the chosen one.
func TestOthers(t *testing.T) {
	entries := []string{
		"work.2030-06-01.token", "work.2025-01-01.token", "work.2024-01-01.token",
		"work.backup.token", "work.2025-01-01.token.bak", "workx.2025-01-01.token",
		"other.2025-01-01.token", "work.extra.2025-01-01.token", "work.token",
	}
	got := Others(entries, "work.2030-06-01.token")
	if want := []string{"work.2024-01-01.token", "work.2025-01-01.token"}; !slices.Equal(got, want) {
		t.Fatalf("Others = %q, want %q", got, want)
	}
	if got := Others([]string{"work.2030-06-01.token"}, "work.2030-06-01.token"); len(got) != 0 {
		t.Fatalf("a name with no other file has others: %q", got)
	}
	if got := Others(entries, "not-a-token"); got != nil {
		t.Fatalf("a chosen file that is not a token file has others: %q", got)
	}
}

func TestShadowWarning(t *testing.T) {
	got := ShadowWarning("/home/u/.claude-tokens/ccy/tokens", "work.2024-01-01.token", []string{"work.2025-01-01.token", "work.2030-06-01.token"})
	want := "⚠  Token work has 2 other file(s): work.2025-01-01.token work.2030-06-01.token\n" +
		"   ccy uses the earliest-dated, work.2024-01-01.token. Remove the file you do not want from /home/u/.claude-tokens/ccy/tokens\n"
	if got != want {
		t.Fatalf("ShadowWarning =\n%q\nwant\n%q", got, want)
	}
	if ShadowWarning("/d", "work.2024-01-01.token", nil) != "" {
		t.Fatal("no others, no warning")
	}
}

func TestEarliest(t *testing.T) {
	got, ok := Earliest([]string{"work.2030-06-01.token", "work.2024-01-01.token", "other.2020-01-01.token", "work.token"}, "work")
	if !ok || got != "work.2024-01-01.token" {
		t.Fatalf("Earliest = %q, %v", got, ok)
	}
	if _, ok := Earliest([]string{"other.2020-01-01.token"}, "work"); ok {
		t.Fatal("a name with no dated file has an earliest")
	}
}
