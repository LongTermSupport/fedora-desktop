package loadgen

import (
	"strings"
	"testing"
)

func TestCheckTargetTakesALabServer(t *testing.T) {
	for _, base := range []string{"http://127.0.0.1:8008", "http://127.9.9.9", "http://[::1]:8448", "http://127.0.0.1:6167/"} {
		if err := CheckTarget(base, "loadgen.test"); err != nil {
			t.Errorf("CheckTarget(%q): %v", base, err)
		}
	}
	if err := CheckTarget("http://127.0.0.1:8008", "hs.lab-2.test"); err != nil {
		t.Errorf("a name of three labels: %v", err)
	}
}

func TestCheckTargetRefusesAnythingElse(t *testing.T) {
	cases := []struct{ base, server, says string }{
		{"", "loadgen.test", "--homeserver"},
		{"http://127.0.0.1:8008", "", "--server-name"},
		{"http://127.0.0.1:8008", "alpha.teamchat.internal", "not under .test"},
		{"http://127.0.0.1:8008", "matrix.org", "not under .test"},
		{"http://127.0.0.1:8008", ".test", "not under .test"},
		{"http://127.0.0.1:8008", "Lab.test", "not under .test"},
		{"http://127.0.0.1:8008", "lab..test", "not under .test"},
		{"http://192.0.2.4:8008", "loadgen.test", "not a loopback IP"},
		{"http://localhost:8008", "loadgen.test", "not a loopback IP"},
		{"http://hs.loadgen.test", "loadgen.test", "not a loopback IP"},
		{"https://127.0.0.1:8448", "loadgen.test", "plain http"},
		{"ftp://127.0.0.1", "loadgen.test", "plain http"},
		{"http://user:pw@127.0.0.1", "loadgen.test", "user, path, query"},
		{"http://127.0.0.1/_matrix", "loadgen.test", "user, path, query"},
		{"http://127.0.0.1/?a=b", "loadgen.test", "user, path, query"},
		{"http://127.0.0.1/#x", "loadgen.test", "user, path, query"},
		{"127.0.0.1:8008", "loadgen.test", "not a URL"},
		{"http://%zz", "loadgen.test", "not a URL"},
	}
	for _, c := range cases {
		err := CheckTarget(c.base, c.server)
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("CheckTarget(%q, %q) = %v, want it refused saying %q", c.base, c.server, err, c.says)
		}
	}
}

func TestCheckServerHoldsTheAnswerToTheName(t *testing.T) {
	if err := CheckServer("@admin:loadgen.test", "loadgen.test"); err != nil {
		t.Fatalf("CheckServer: %v", err)
	}
	for _, id := range []string{"@admin:alpha.teamchat.internal", "@admin:loadgen.test.evil", "admin", "@admin"} {
		if err := CheckServer(id, "loadgen.test"); err == nil || !strings.Contains(err.Error(), "is not on loadgen.test") {
			t.Errorf("CheckServer(%q) = %v, want refused", id, err)
		}
	}
}
