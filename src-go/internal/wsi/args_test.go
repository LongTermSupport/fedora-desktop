package wsi

import "testing"

func TestW1TheDefaults(t *testing.T) {
	o, err := parseArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := options{claudeModel: "sonnet", claudeStyle: "corporate"}
	if o != want {
		t.Fatalf("got %+v", o)
	}
}

func TestW1EveryOptionInBothSpellings(t *testing.T) {
	o, err := parseArgs([]string{
		"-c", "-a", "--no-auto-enter", "--paste-with-shift", "1", "-w", "-l", "de", "--no-notify",
		"--claude-process", "--claude-model", "opus", "--claude-style", "natural", "--session-token", "tok", "-v",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := options{
		clipboard: true, autoPaste: true, skipEnter: true, pasteWithShift: "1", wrap: true, language: "de",
		noNotify: true, claude: true, claudeModel: "opus", claudeStyle: "natural", token: "tok", verbose: true,
	}
	if o != want {
		t.Fatalf("got  %+v\nwant %+v", o, want)
	}
	long, err := parseArgs([]string{"--clipboard", "--auto-paste", "--wrap-marker", "--language", "en", "--debug"})
	if err != nil {
		t.Fatal(err)
	}
	if !long.clipboard || !long.autoPaste || !long.wrap || long.language != "en" || !long.debug || !long.verbose {
		t.Fatalf("got %+v", long)
	}
}

func TestW1AServerAddressAndNetapi(t *testing.T) {
	o, err := parseArgs([]string{"server.local:8080"})
	if err != nil || o.server != "server.local:8080" {
		t.Fatalf("got %+v, %v", o, err)
	}
	for _, flag := range []string{"-n", "--netapi"} {
		o, err = parseArgs([]string{"host:1", flag})
		if err != nil || o.server != "127.0.0.1:58080" {
			t.Fatalf("%s: got %+v, %v", flag, o, err)
		}
	}
}

func TestW2HelpStopsReadingUnlessAnErrorCameFirst(t *testing.T) {
	o, err := parseArgs([]string{"-h", "--bogus"})
	if err != nil || !o.help {
		t.Fatalf("got %+v, %v", o, err)
	}
	if _, err = parseArgs([]string{"--bogus", "--help"}); err == nil {
		t.Fatal("an error before --help wins")
	}
}

func TestW3ArgumentErrorsAndTheWholeLineIsStillRead(t *testing.T) {
	cases := map[string][]string{
		"--language requires a value":                                        {"--debug", "--language"},
		"--paste-with-shift requires 0 (Ctrl+V) or 1 (Ctrl+Shift+V), got: 2": {"--paste-with-shift", "2", "--session-token", "tok"},
		"unknown option: --frob (try --help)":                                {"--frob", "--session-token", "tok", "--debug"},
		"--session-token requires a value":                                   {"--session-token"},
		"--claude-model requires a value":                                    {"--claude-model"},
		"--claude-style requires a value":                                    {"--claude-style"},
		"--paste-with-shift requires a value":                                {"--paste-with-shift"},
	}
	for want, args := range cases {
		o, err := parseArgs(args)
		if err == nil || err.Error() != want {
			t.Errorf("%q: got %v, want %q", args, err, want)
		}
		if len(args) > 2 && args[len(args)-1] == "--debug" && (!o.debug || o.token != "tok") {
			t.Errorf("%q: the rest of the line was not read: %+v", args, o)
		}
		if args[0] == "--paste-with-shift" && len(args) == 4 && o.token != "tok" {
			t.Errorf("%q: the token after the error was not read: %+v", args, o)
		}
	}
}
