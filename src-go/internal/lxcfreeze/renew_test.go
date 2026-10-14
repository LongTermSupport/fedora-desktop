package lxcfreeze

// The DHCP renewal after a thaw. Its end-to-end cases (each message, the stop at the first failing
// device, the pipes rule) are in act_test.go beside the unfreeze they follow; this is the parse of
// what NetworkManager reports.

import "testing"

func TestEthernetDevicesAreTheFirstFieldOfEachEthernetLine(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   []string
	}{
		{"ethernet among others", "eth0:ethernet\nlo:loopback\nwlan0:wifi\n", []string{"eth0"}},
		{"several, in order", "eth1:ethernet\neth0:ethernet\n", []string{"eth1", "eth0"}},
		{"none", "lo:loopback\n", nil},
		{"nothing at all", "", nil},
		{"a line with no colon is not a record", "ethernet\n", nil},
		{"the type is the second field, not a substring", "eth0:ethernet-bond\nbr0:bridge\n", nil},
		{"a third field does not matter", "eth0:ethernet:connected\n", []string{"eth0"}},
		{"the device name is not the type", "ethernet:wifi\n", nil},
	}
	for _, c := range cases {
		eqStrings(t, c.name, ethernetDevices(c.status), c.want)
	}
}
