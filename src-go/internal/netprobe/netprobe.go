// Package netprobe holds the pure parsers and builders for what the installer asks the
// network tools: ss listings, ip route JSON, firewall-cmd zone answers and nmcli address
// output. Nothing here runs a command or touches the host.
package netprobe

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// BoundAddresses are the sorted, unique local addresses in `ss -ltnH` output: the fourth
// field of each row, without its port, brackets or zone id. A row with fewer than four
// fields is skipped.
func BoundAddresses(out string) []string {
	seen := map[string]bool{}
	var addrs []string
	for _, line := range lines(out) {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3]
		if i := strings.LastIndex(local, ":"); i >= 0 {
			local = local[:i]
		}
		local = strings.TrimPrefix(local, "[")
		local = strings.TrimSuffix(local, "]")
		if i := strings.Index(local, "%"); i >= 0 {
			local = local[:i]
		}
		if local != "" && !seen[local] {
			seen[local] = true
			addrs = append(addrs, local)
		}
	}
	slices.Sort(addrs)
	return addrs
}

// ExpectedAddresses are the sorted, unique addresses a homeserver port may be bound on:
// loopback and its listen addresses.
func ExpectedAddresses(listen []string) []string {
	all := append([]string{"127.0.0.1"}, listen...)
	slices.Sort(all)
	return slices.Compact(all)
}

// AssertListening is nil when the listing shows the port bound on exactly loopback and the
// listen addresses.
func AssertListening(port int, ssOut string, listen []string) error {
	got := BoundAddresses(ssOut)
	want := ExpectedAddresses(listen)
	if !slices.Equal(got, want) {
		return fmt.Errorf("port %d is bound on [%s], not exactly [%s]", port, strings.Join(got, " "), strings.Join(want, " "))
	}
	return nil
}

// lines splits on newlines and drops a trailing carriage return from each.
func lines(out string) []string {
	var all []string
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		all = append(all, strings.TrimRight(sc.Text(), "\r"))
	}
	return all
}

// ErrNoRoute is returned when `ip -j route get` names no device.
var ErrNoRoute = errors.New("no route")

const maxInterfaceName = 15

// RouteDev is the device of the first route in `ip -j route get` JSON. The name goes into a
// firewall-cmd argument, so it must look like an interface name: 1 to 15 bytes, no slash,
// space, control character or "=", not "." or "..", and no leading "-".
func RouteDev(data []byte) (string, error) {
	var routes []map[string]any
	if err := json.Unmarshal(data, &routes); err != nil {
		return "", fmt.Errorf("ip route get: unreadable JSON: %w", err)
	}
	if len(routes) == 0 || routes[0] == nil {
		return "", ErrNoRoute
	}
	raw, present := routes[0]["dev"]
	if !present || raw == nil {
		return "", ErrNoRoute
	}
	dev, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("ip route get: dev is %T, not a string", raw)
	}
	if dev == "" {
		return "", ErrNoRoute
	}
	if !validInterface(dev) {
		return "", fmt.Errorf("ip route get: %q is not an interface name", dev)
	}
	return dev, nil
}

func validInterface(name string) bool {
	if len(name) > maxInterfaceName || name == "." || name == ".." || strings.HasPrefix(name, "-") {
		return false
	}
	return !strings.ContainsFunc(name, func(r rune) bool {
		return r <= ' ' || r == 0x7f || r == '/' || r == '='
	})
}

var zonePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ParseZone reads the answer of `firewall-cmd --get-zone-of-interface`. The text "no zone"
// means the interface has none, whatever the exit status was; anything else must be one
// zone name.
func ParseZone(out string) (string, bool, error) {
	text := strings.TrimSpace(out)
	if text == "no zone" {
		return "", true, nil
	}
	if !zonePattern.MatchString(text) {
		return "", false, fmt.Errorf("firewall-cmd answered %q, not a zone name", text)
	}
	return text, false, nil
}

// HasLine reports whether out has a line equal to name (grep -Fx).
func HasLine(out, name string) bool {
	return slices.Contains(lines(out), name)
}

// Settings are the nmcli connection settings that give the dummy interface exactly one
// address: a /32 for IPv4 (IPv6 disabled), a /128 for IPv6 (IPv4 disabled).
func Settings(addr netip.Addr) []string {
	if addr.Is6() {
		return []string{
			"ipv4.method", "disabled", "ipv4.addresses", "",
			"ipv6.method", "manual", "ipv6.addresses", netip.PrefixFrom(addr, 128).String(),
		}
	}
	return []string{
		"ipv4.method", "manual", "ipv4.addresses", netip.PrefixFrom(addr, 32).String(),
		"ipv6.method", "disabled", "ipv6.addresses", "",
	}
}

// AddressesMatch reports whether the output of
// `nmcli -g ipv4.addresses,ipv6.addresses connection show` holds exactly the one address
// of Settings and nothing else. The output is compared as parsed prefixes, not as text:
// terse mode escapes ":" in IPv6 values, several addresses may be listed with commas or
// pipes, and an empty family prints as "--" or blank. A token that is not a prefix is an
// error, never an assumed mismatch.
func AddressesMatch(out string, addr netip.Addr) (bool, error) {
	want := netip.PrefixFrom(addr, addr.BitLen())
	var found []netip.Prefix
	for _, line := range lines(out) {
		for _, token := range splitAddresses(line) {
			prefix, err := netip.ParsePrefix(token)
			if err != nil {
				return false, fmt.Errorf("nmcli addresses: %q is not an address with a prefix", token)
			}
			found = append(found, prefix)
		}
	}
	return len(found) == 1 && found[0] == want, nil
}

// splitAddresses unescapes one terse-mode line and splits it into its address tokens.
func splitAddresses(line string) []string {
	line = strings.ReplaceAll(line, `\:`, ":")
	line = strings.ReplaceAll(line, `\\`, `\`)
	tokens := strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == '|' || r == ' ' || r == '\t' })
	return slices.DeleteFunc(tokens, func(t string) bool { return t == "--" })
}
