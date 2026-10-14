package busaddr

import (
	"errors"
	"strings"
	"testing"
)

func TestValidAccepts(t *testing.T) {
	for _, text := range []string{
		"192.0.2.40",
		"198.51.100.1",
		"203.0.113.7",
		"1.1.1.1",
		"2001:db8::1",
		"fd00::1",
		"fd12:3456:789a::7",
		"2001:db8:1:2:3:4:5:6",
		// Not in the bash list of refusals, kept for parity: broadcast and the "this network" block.
		"255.255.255.255",
		"0.1.2.3",
		// IPv4-compatible (deprecated) IPv6 form in canonical text.
		"::102:304",
	} {
		t.Run(text, func(t *testing.T) {
			if err := Check(text); err != nil {
				t.Fatalf("Check(%q) = %v, want accepted", text, err)
			}
			if !Valid(text) {
				t.Fatalf("Valid(%q) = false", text)
			}
		})
	}
}

func TestValidRefuses(t *testing.T) {
	cases := []struct {
		name, text string
		reason     error
	}{
		{"empty", "", ErrNotIP},
		{"word", "localhost", ErrNotIP},
		{"hostname", "example.com", ErrNotIP},
		{"cidr v4", "192.0.2.1/32", ErrNotIP},
		{"cidr v6", "2001:db8::1/128", ErrNotIP},
		{"port", "192.0.2.1:80", ErrNotIP},
		{"bracketed v6", "[2001:db8::1]", ErrNotIP},
		{"leading space", " 192.0.2.1", ErrNotIP},
		{"trailing space", "192.0.2.1 ", ErrNotIP},
		{"trailing newline", "192.0.2.1\n", ErrNotIP},
		{"embedded newline", "192.0.2.1\n192.0.2.2", ErrNotIP},
		{"nul", "192.0.2.1\x00", ErrNotIP},
		{"nul in middle", "192.0\x00.2.1", ErrNotIP},
		{"leading zero octet", "192.00.2.1", ErrNotIP},
		{"leading zero first", "0192.0.2.1", ErrNotIP},
		{"octal looking", "0177.0.0.1", ErrNotIP},
		{"hex octet", "0x7f.0.0.1", ErrNotIP},
		{"decimal integer", "3221225985", ErrNotIP},
		{"short dotted", "192.0.2", ErrNotIP},
		{"too many octets", "192.0.2.1.1", ErrNotIP},
		{"octet over 255", "192.0.2.256", ErrNotIP},
		{"negative octet", "192.0.2.-1", ErrNotIP},
		{"plus sign", "+192.0.2.1", ErrNotIP},
		{"option injection", "--help", ErrNotIP},
		{"shell injection", "192.0.2.1;reboot", ErrNotIP},
		{"command substitution", "$(id)", ErrNotIP},
		{"unicode digits", "１９２.0.2.1", ErrNotIP},
		{"unspecified v4", "0.0.0.0", ErrUnusable},
		{"unspecified v6", "::", ErrUnusable},
		{"loopback v4", "127.0.0.1", ErrUnusable},
		{"loopback v4 other", "127.1.2.3", ErrUnusable},
		{"loopback v6", "::1", ErrUnusable},
		{"link-local v4", "169.254.1.1", ErrUnusable},
		{"link-local v4 low", "169.254.0.0", ErrUnusable},
		{"link-local v6", "fe80::1", ErrUnusable},
		{"link-local v6 top", "febf::1", ErrUnusable},
		{"multicast v4", "224.0.0.1", ErrUnusable},
		{"multicast v4 top", "239.255.255.255", ErrUnusable},
		{"multicast v6", "ff02::1", ErrUnusable},
		{"multicast v6 node-local", "ff01::1", ErrUnusable},
		{"multicast v6 global", "ff0e::1", ErrUnusable},
		// B2: a zone id is not part of an address nmcli accepts; the bash accepted it.
		{"zone id", "2001:db8::1%eth0", ErrZone},
		{"zone id numeric", "2001:db8::1%2", ErrZone},
		{"zone id on link-local", "fe80::1%eth0", ErrZone},
		{"zone id empty", "2001:db8::1%", ErrNotIP},
		{"zone id on v4", "192.0.2.1%eth0", ErrNotIP},
		// B2: IPv4-mapped forms depended on the Python version; refused always.
		{"mapped dotted", "::ffff:192.0.2.1", ErrMapped},
		{"mapped hex", "::ffff:c000:201", ErrMapped},
		{"mapped full", "0:0:0:0:0:ffff:c000:201", ErrMapped},
		{"non-canonical uppercase", "2001:DB8::1", ErrNotCanonical},
		{"non-canonical zero run", "2001:db8:0:0:0:0:0:1", ErrNotCanonical},
		{"non-canonical leading zero group", "2001:0db8::1", ErrNotCanonical},
		{"non-canonical double colon short", "2001:db8:0::1", ErrNotCanonical},
		{"non-canonical full form", "fd00:0:0:0:0:0:0:1", ErrNotCanonical},
		{"too many groups", "1:2:3:4:5:6:7:8:9", ErrNotIP},
		{"two double colons", "2001::db8::1", ErrNotIP},
		{"triple colon", ":::", ErrNotIP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.text)
			if err == nil {
				t.Fatalf("Check(%q) accepted", tc.text)
			}
			if !errors.Is(err, tc.reason) {
				t.Fatalf("Check(%q) = %v, want reason %v", tc.text, err, tc.reason)
			}
			if Valid(tc.text) {
				t.Fatalf("Valid(%q) = true", tc.text)
			}
		})
	}
}

func TestCheckMessageNamesTheText(t *testing.T) {
	err := Check("::1")
	if err == nil || !strings.Contains(err.Error(), "::1") {
		t.Fatalf("error %v does not name the address", err)
	}
}

// What Parse returns prints as the text that was validated, so what is later handed to
// nmcli is what was checked.
func TestAcceptedTextIsCanonical(t *testing.T) {
	for _, text := range []string{"192.0.2.40", "2001:db8::1", "fd00::1"} {
		addr, err := Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		if addr.String() != text {
			t.Fatalf("Parse(%q).String() = %q", text, addr.String())
		}
	}
	if _, err := Parse("::1"); err == nil {
		t.Fatal("Parse accepted loopback")
	}
}
