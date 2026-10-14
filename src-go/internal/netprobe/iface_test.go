package netprobe

import (
	"errors"
	"slices"
	"testing"
)

const addrJSON = `[{"ifname":"lo","addr_info":[{"local":"127.0.0.1"}]},
{"ifname":"agentbus0","addr_info":[{"local":"192.0.2.10"}]},
{"ifname":"wg0","addr_info":[{"local":"2001:db8::1"},{"local":"192.0.2.77"}]},
{"ifname":"down0"},
{"ifname":"dup1","addr_info":[{"local":"198.51.100.5"}]},
{"ifname":"dup0","addr_info":[{"local":"198.51.100.5"},{"local":"198.51.100.5"}]}]`

func TestInterfaceOfFindsTheOneInterfaceCarryingTheAddress(t *testing.T) {
	for addr, want := range map[string]string{
		"192.0.2.10":  "agentbus0",
		"2001:db8::1": "wg0",
		"192.0.2.77":  "wg0",
		"127.0.0.1":   "lo",
	} {
		got, err := InterfaceOf([]byte(addrJSON), addr)
		if err != nil || got != want {
			t.Errorf("InterfaceOf(%s) = %q, %v; want %q", addr, got, err, want)
		}
	}
}

func TestInterfaceOfReportsNoInterface(t *testing.T) {
	_, err := InterfaceOf([]byte(addrJSON), "198.51.100.7")
	if !errors.Is(err, ErrNoInterface) {
		t.Fatalf("got %v", err)
	}
}

func TestInterfaceOfReportsEveryInterfaceWhenThereAreSeveral(t *testing.T) {
	_, err := InterfaceOf([]byte(addrJSON), "198.51.100.5")
	many, ok := errors.AsType[*MultipleInterfacesError](err)
	if !ok || !slices.Equal(many.Names, []string{"dup0", "dup1"}) {
		t.Fatalf("got %v", err)
	}
	if many.Error() != "on more than one interface: dup0, dup1" {
		t.Fatalf("text %q", many.Error())
	}
}

func TestInterfaceOfRefusesJSONItCannotRead(t *testing.T) {
	for _, data := range []string{"", "not json", `{"ifname":"lo"}`, `[{"ifname":3}]`, `[{"ifname":"lo","addr_info":[{"local":4}]}]`} {
		if _, err := InterfaceOf([]byte(data), "192.0.2.10"); err == nil || errors.Is(err, ErrNoInterface) {
			t.Errorf("InterfaceOf(%q) = %v", data, err)
		}
	}
}
