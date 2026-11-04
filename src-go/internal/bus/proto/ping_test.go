package proto

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

const (
	testSHA    = "0123456789abcdef0123456789abcdef01234567"
	testCoord  = "@app.1+desk.podman:server.test"
	testWorker = "@app.2+desk.podman:server.test"
	testAlice  = "@alice:server.test"
)

func testContext() *Context {
	return &Context{
		ServerName: "server.test",
		Humans:     []string{testAlice},
		Roles:      map[string]string{testCoord: RoleCoordinator, testWorker: RoleWorker},
		Repos:      map[string][]string{"example-org/myrepo": {"main"}},
		Org:        "example-org",
		Exclude:    []string{"private"},
	}
}

func TestRenderPingIsTheSpecsLine(t *testing.T) {
	got := RenderPing(VerbFetch, []string{testWorker, testCoord}, PingFields{Ref: "commit:a/b@x", Branch: "main"})
	want := "[agent-bus] fetch commit:a/b@x on main -> " + testCoord + " " + testWorker
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := RenderPing(VerbAck, []string{testCoord}, PingFields{Re: "$e"}); got != "[agent-bus] ack - -> "+testCoord+" re $e" {
		t.Fatalf("got %q", got)
	}
}

func TestBuiltPingValidates(t *testing.T) {
	ref := "commit:example-org/myrepo@" + testSHA
	content := BuildPing(VerbFetch, []string{testWorker}, PingFields{Ref: ref, Branch: "main"})
	ping, err := ValidatePingContent(content, testContext())
	if err != nil {
		t.Fatal(err)
	}
	if ping.Verb != VerbFetch || ping.Ref == nil || ping.Ref.Text != ref || ping.Trust != TrustTrusted ||
		ping.Branch != "main" {
		t.Fatalf("%+v", ping)
	}
	content["body"] = "x"
	var refusal *RefusalError
	if _, err := ValidatePingContent(content, testContext()); !errors.As(err, &refusal) ||
		refusal.Reason != ReasonBody {
		t.Fatalf("%v", err)
	}
}

func TestPingVersionComesFirst(t *testing.T) {
	content := pyjson.Object{
		PingKey: pyjson.Object{"v": pyjson.Number{Lit: "2", Kind: pyjson.Int}}, "m.new_content": pyjson.Object{},
	}
	if _, err := ValidatePingContent(content, testContext()); err == nil || err.Error() != "version" {
		t.Fatalf("%v", err)
	}
}
