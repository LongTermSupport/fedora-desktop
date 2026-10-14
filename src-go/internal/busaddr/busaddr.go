// Package busaddr decides whether text may be the address of the agent-bus dummy
// interface (agentbus0): a concrete, canonical IP literal that is never a wildcard,
// loopback, link-local or multicast address, and never carries a zone id or an
// IPv4-mapped form. The text is handed to nmcli and becomes a listen address, so what
// passes here is exactly what is later used.
package busaddr

import (
	"errors"
	"fmt"
	"net/netip"
)

// The reasons Check refuses; errors.Is tells them apart.
var (
	ErrNotIP        = errors.New("not an IP address")
	ErrZone         = errors.New("a zone id is not allowed")
	ErrMapped       = errors.New("an IPv4-mapped IPv6 form is not allowed")
	ErrNotCanonical = errors.New("not in canonical form")
	ErrUnusable     = errors.New("a wildcard, loopback, link-local or multicast address is not allowed")
)

// Parse returns the address in text, or the reason it may not be used.
func Parse(text string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("%q: %w", text, ErrNotIP)
	}
	switch {
	case addr.Zone() != "":
		return netip.Addr{}, fmt.Errorf("%q: %w", text, ErrZone)
	case addr.Is4In6():
		return netip.Addr{}, fmt.Errorf("%q: %w", text, ErrMapped)
	case addr.String() != text:
		return netip.Addr{}, fmt.Errorf("%q: %w (%s)", text, ErrNotCanonical, addr)
	case addr.IsUnspecified() || addr.IsLoopback() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsMulticast():
		return netip.Addr{}, fmt.Errorf("%q: %w", text, ErrUnusable)
	}
	return addr, nil
}

// Check is nil when text is usable as the bus address.
func Check(text string) error {
	_, err := Parse(text)
	return err
}

// Valid reports whether text is usable as the bus address.
func Valid(text string) bool {
	return Check(text) == nil
}
