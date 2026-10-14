package hostnet

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netprobe"
)

func newNet() (*Net, *execx.Fake) {
	f := &execx.Fake{}
	return &Net{Runner: f, Chatter: &strings.Builder{}}, f
}

func chatterOf(n *Net) string {
	return fmt.Sprint(n.Chatter)
}

func lastLine(t *testing.T, f *execx.Fake) string {
	t.Helper()
	calls := f.Calls()
	if len(calls) == 0 {
		t.Fatal("nothing was run")
	}
	return calls[len(calls)-1].Line()
}

func TestAddrJSONRunsIpOnce(t *testing.T) {
	n, f := newNet()
	f.On([]string{"ip", "-j", "addr", "show"}, execx.Result{Stdout: []byte("[]")})
	got, err := n.AddrJSON(context.Background())
	if err != nil || string(got) != "[]" || lastLine(t, f) != "ip -j addr show" {
		t.Fatalf("%q, %v, %q", got, err, lastLine(t, f))
	}
	f.On([]string{"ip"}, execx.Result{ExitCode: 1})
	if _, err := n.AddrJSON(context.Background()); err == nil || !strings.Contains(err.Error(), "ip -j addr show") {
		t.Fatalf("got %v", err)
	}
}

func TestRouteDevParsesTheDeviceAndKeepsNoRouteRecognisable(t *testing.T) {
	n, f := newNet()
	f.On([]string{"ip", "-j", "route", "get", "198.51.100.0"}, execx.Result{Stdout: []byte(`[{"dst":"198.51.100.0","dev":"wg0"}]`)})
	dev, err := n.RouteDev(context.Background(), "198.51.100.0")
	if err != nil || dev != "wg0" {
		t.Fatalf("%q, %v", dev, err)
	}
	f.On([]string{"ip", "-j", "route", "get", "203.0.113.1"}, execx.Result{Stdout: []byte(`[]`)})
	if _, err := n.RouteDev(context.Background(), "203.0.113.1"); !errors.Is(err, netprobe.ErrNoRoute) {
		t.Fatalf("got %v", err)
	}
	f.On([]string{"ip", "-j", "route", "get", "192.0.2.1"}, execx.Result{ExitCode: 2, Stderr: []byte("unreachable")})
	if _, err := n.RouteDev(context.Background(), "192.0.2.1"); err == nil || errors.Is(err, netprobe.ErrNoRoute) {
		t.Fatalf("got %v", err)
	}
}

func TestListeningAsksSsForOnePortWithOneArgument(t *testing.T) {
	n, f := newNet()
	f.On([]string{"ss"}, execx.Result{Stdout: []byte("LISTEN 0 4096 127.0.0.1:8448 0.0.0.0:*\n")})
	got, err := n.Listening(context.Background(), 8448)
	if err != nil || !strings.HasPrefix(got, "LISTEN") {
		t.Fatalf("%q, %v", got, err)
	}
	call := f.Calls()[0]
	if call.Name != "ss" || len(call.Args) != 2 || call.Args[1] != "sport = :8448" {
		t.Fatalf("argv %v", call.Args)
	}
	f.On([]string{"ss"}, execx.Result{ExitCode: 1})
	if _, err := n.Listening(context.Background(), 8448); err == nil {
		t.Fatal("a failed ss returned no error")
	}
}

func TestConnectionExistsReadsTheNamesOfTheConnections(t *testing.T) {
	n, f := newNet()
	f.On([]string{"nmcli", "-t", "-f", "NAME", "connection", "show"}, execx.Result{Stdout: []byte("Wired connection 1\nagentbus0\n")})
	if found, err := n.ConnectionExists(context.Background(), "agentbus0"); err != nil || !found {
		t.Fatalf("%v, %v", found, err)
	}
	if found, err := n.ConnectionExists(context.Background(), "agentbus"); err != nil || found {
		t.Fatalf("a prefix is not a name: %v, %v", found, err)
	}
	f.On([]string{"nmcli"}, execx.Result{ExitCode: 8})
	if _, err := n.ConnectionExists(context.Background(), "agentbus0"); err == nil {
		t.Fatal("a failed nmcli returned no error")
	}
}

func TestConnectionOperationsRunTheExactArgvAndSendChatterAside(t *testing.T) {
	n, f := newNet()
	f.On([]string{"nmcli"}, execx.Result{Stdout: []byte("done\n")})
	ctx := context.Background()
	settings := []string{"ipv4.method", "manual", "ipv4.addresses", "192.0.2.10/32"}
	ops := []func() error{
		func() error { return n.AddDummy(ctx, "agentbus0", settings) },
		func() error { return n.Modify(ctx, "agentbus0", settings) },
		func() error { return n.Up(ctx, "agentbus0") },
	}
	wants := []string{
		"nmcli connection add type dummy ifname agentbus0 con-name agentbus0 connection.autoconnect yes ipv4.method manual ipv4.addresses 192.0.2.10/32",
		"nmcli connection modify agentbus0 ipv4.method manual ipv4.addresses 192.0.2.10/32",
		"nmcli connection up agentbus0",
	}
	for i := range ops {
		if err := ops[i](); err != nil {
			t.Fatalf("%s: %v", wants[i], err)
		}
		if got := lastLine(t, f); got != wants[i] {
			t.Fatalf("ran %q, want %q", got, wants[i])
		}
	}
	if strings.Count(chatterOf(n), "done") != len(ops) {
		t.Fatalf("chatter %q", chatterOf(n))
	}
	f.On([]string{"nmcli"}, execx.Result{ExitCode: 4})
	for i := range ops {
		if err := ops[i](); err == nil || !strings.Contains(err.Error(), "nmcli connection") {
			t.Errorf("got %v", err)
		}
	}
}

func TestAddressesAndStateAreReadAsTerseValues(t *testing.T) {
	n, f := newNet()
	f.On([]string{"nmcli", "-g", "ipv4.addresses,ipv6.addresses", "connection", "show", "agentbus0"}, execx.Result{Stdout: []byte("192.0.2.10/32\n\n")})
	f.On([]string{"nmcli", "-g", "GENERAL.STATE", "connection", "show", "agentbus0"}, execx.Result{Stdout: []byte("activated\n")})
	if got, err := n.Addresses(context.Background(), "agentbus0"); err != nil || got != "192.0.2.10/32\n\n" {
		t.Fatalf("%q, %v", got, err)
	}
	if got, err := n.State(context.Background(), "agentbus0"); err != nil || got != "activated" {
		t.Fatalf("%q, %v", got, err)
	}
	f.On([]string{"nmcli"}, execx.Result{ExitCode: 10})
	if _, err := n.Addresses(context.Background(), "agentbus0"); err == nil {
		t.Fatal("a failed nmcli returned no error")
	}
	if _, err := n.State(context.Background(), "agentbus0"); err == nil {
		t.Fatal("a failed nmcli returned no error")
	}
}

func TestDownloadUsesHTTPSOnlyAndWritesToTheGivenFile(t *testing.T) {
	n, f := newNet()
	f.On([]string{"curl"}, execx.Result{})
	if err := n.Download(context.Background(), "https://example.com/a.zst", "/work/a.zst"); err != nil {
		t.Fatal(err)
	}
	want := "curl -q --fail --silent --show-error --location --proto =https --tlsv1.2 --max-time 600 -o /work/a.zst https://example.com/a.zst"
	if got := lastLine(t, f); got != want {
		t.Fatalf("ran %q", got)
	}
	f.On([]string{"curl"}, execx.Result{ExitCode: 78, Stderr: []byte("not found")})
	err := n.Download(context.Background(), "https://example.com/a.zst", "/work/a.zst")
	exit, ok := errors.AsType[*execx.ExitError](err)
	if !ok || exit.Code != 78 || !strings.Contains(err.Error(), "curl") {
		t.Fatalf("got %v", err)
	}
}

func TestProbeReturnsTheMergedOutputWithTheFailure(t *testing.T) {
	n, f := newNet()
	f.On([]string{"curl"}, execx.Result{Stdout: []byte(`{"name":"Tuwunel"}`)})
	out, err := n.Probe(context.Background(), "http://127.0.0.1:8448/_tuwunel/server_version")
	if err != nil || !strings.Contains(out, "Tuwunel") {
		t.Fatalf("%q, %v", out, err)
	}
	want := "curl -q --fail --silent --show-error --noproxy * --max-time 2 http://127.0.0.1:8448/_tuwunel/server_version"
	if got := lastLine(t, f); got != want {
		t.Fatalf("ran %q", got)
	}
	f.On([]string{"curl"}, execx.Result{ExitCode: 7, Stderr: []byte("curl: (7) Failed to connect")})
	out, err = n.Probe(context.Background(), "http://127.0.0.1:8448/x")
	if err == nil || !strings.Contains(out, "Failed to connect") {
		t.Fatalf("%q, %v", out, err)
	}
}
