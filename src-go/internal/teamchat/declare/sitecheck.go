package declare

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// SiteIsAHostname is the fixed refusal of a declared site that spells a name the code can see.
// It is best effort: a machine is known by names the code cannot read (an alias, a name in a
// DNS zone), so the guarantee is that no code reads a hostname into a handle (SPEC-cross-network
// X26, X27), not that every hostname is caught here.
const SiteIsAHostname = "must not be the machine's hostname, a dot-label of it, or a DNS label of a team's url host " +
	"(best effort: it cannot see every name the machine is known by; the guarantee is that no code reads a hostname into a handle)"

// errNoHostname is a machine whose hostname the seam gave as nothing.
var errNoHostname = errors.New("the machine's hostname is empty")

// CheckSites refuses a declared site, the machine's teamchat_site and each LXC entry's own, that
// is the machine's hostname (normalised), any dot-label of it, or a DNS label of the url host of
// a present team (an address has no labels). The hostname arrives through the seam, MachineHostname
// in production and a fake in a test, and is asked only when a site is declared. A hostname that
// cannot be read, or is empty, fails the check: the check does not pass without it.
//
// A handle already issued with a hostname in it is a permanent identity (X28): this refuses a
// site declared from now on and does not look at handles. A site is judged against every present
// team's url host, however many teams the machine joins, so an entry's own site is held to the
// url of teams other than its own too: the owner picks another site.
func (d Declaration) CheckSites(hostname func() (string, error)) error {
	declared := d.Site != ""
	for _, m := range d.LXC {
		declared = declared || m.Site != ""
	}
	if !declared {
		return nil
	}
	name, err := hostname()
	if err != nil {
		return fmt.Errorf("declaration: the machine's hostname: %w", err)
	}
	if strings.Trim(name, " .") == "" {
		return fmt.Errorf("declaration: %w", errNoHostname)
	}
	names := hostnameSpellings(name)
	for _, t := range d.Teams {
		for _, label := range urlHostLabels(t.URL) {
			names[label] = true
		}
	}
	if names[d.Site] {
		return &FieldError{Field: HostVarSite, Problem: SiteIsAHostname}
	}
	for i, m := range d.LXC {
		if m.Site != d.Site && names[m.Site] {
			return &FieldError{Field: fmt.Sprintf("%s[%d].%s", paths.HostVarLXCMembers, i, keySite), Problem: SiteIsAHostname}
		}
	}
	return nil
}

// hostnameSpellings are the sites a hostname spells: the whole name and each dot-label, each
// normalised as a site is, those that make a site at all.
func hostnameSpellings(name string) map[string]bool {
	name = strings.TrimSuffix(name, ".")
	spelled := map[string]bool{}
	for _, part := range append(strings.Split(name, "."), name) {
		if site, ok := proto.NormaliseSite(part); ok {
			spelled[site] = true
		}
	}
	return spelled
}

// urlHostLabels are the sites the DNS labels of a team's url host spell; none for an address, a
// url that does not parse (the team's own reader has refused it) or no url (an absent team).
func urlHostLabels(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	host := u.Hostname()
	if _, err = netip.ParseAddr(host); err == nil {
		return nil
	}
	var labels []string
	for label := range strings.SplitSeq(strings.TrimSuffix(host, "."), ".") {
		if site, ok := proto.NormaliseSite(label); ok {
			labels = append(labels, site)
		}
	}
	return labels
}
