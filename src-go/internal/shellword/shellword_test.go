package shellword

import "testing"

func TestQuoteLeavesAPlainWordAsItIs(t *testing.T) {
	for _, s := range []string{"magick", "/usr/bin/x", "a.b-c_d", "rgba:1,2", "%fx", "-quality", "+30+30", "user@host"} {
		if got := Quote(s); got != s {
			t.Errorf("Quote(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestQuoteSingleQuotesAnythingElse(t *testing.T) {
	cases := map[string]string{
		"":               "''",
		"John Smith":     "'John Smith'",
		"it's":           `'it'\''s'`,
		"(":              "'('",
		"rgba(0,0,0,1)":  "'rgba(0,0,0,1)'",
		"$HOME":          "'$HOME'",
		"a\nb":           "'a\nb'",
		"color 0,0 xxxx": "'color 0,0 xxxx'",
		"%[fx:w*0.03]":   "'%[fx:w*0.03]'",
		"a?":             "'a?'",
	}
	for in, want := range cases {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestJoinQuotesEachWordAndSeparatesWithOneSpace(t *testing.T) {
	got := Join([]string{"exiftool", "-EXIF:Artist=A B", "", "out.jpg"})
	if want := "exiftool '-EXIF:Artist=A B' '' out.jpg"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if Join(nil) != "" {
		t.Fatal("an empty argv is an empty line")
	}
}
