package wsi

import "testing"

func TestW22TheTranscriptIsCleaned(t *testing.T) {
	cases := map[string]string{
		"hello world\n\n":                      "Hello world",
		" (music) hello [BLANK_AUDIO] there\n": "Hello  there",
		"[noise]\n  ok then":                   "Ok then",
		"(a)(b)[c]":                            "",
		"\n\n\nthird":                          "Third",
		"élan vital":                           "Élan vital",
		"one (two\nthree) four":                "One (two\nthree) four",
		"keep trailing space  ":                "Keep trailing space  ",
		"   \t  ":                              "",
		"line one [x]\n[y] line two\n\n\n":     "Line one \n line two",
	}
	for in, want := range cases {
		if got := cleanTranscript(in); got != want {
			t.Errorf("cleanTranscript(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestW28ThePreviewIsSixtyCharacters(t *testing.T) {
	short := "short"
	if preview(short) != short {
		t.Fatal(preview(short))
	}
	exact := "123456789012345678901234567890123456789012345678901234567890"
	if preview(exact) != exact {
		t.Fatal(preview(exact))
	}
	if got := preview(exact + "é"); got != exact+"..." {
		t.Fatal(got)
	}
	if got := preview("ééééééééééééééééééééééééééééééééééééééééééééééééééééééééééééé"); got != "éééééééééééééééééééééééééééééééééééééééééééééééééééééééééééé..." {
		t.Fatalf("by characters, not bytes: %q", got)
	}
}
