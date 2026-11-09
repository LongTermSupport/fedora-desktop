package declare

import (
	"errors"
	"strings"
	"testing"
)

// fakeHostname is the hostname seam of a test: the name, and the number of times it was asked.
type fakeHostname struct {
	name  string
	err   error
	calls int
}

func (f *fakeHostname) read() (string, error) {
	f.calls++
	return f.name, f.err
}

// siteDecl is validDecl with the machine's site and the first LXC entry's own.
func siteDecl(machine, entry string) map[string]any {
	d := validDecl()
	if machine != "" {
		d["teamchat_site"] = machine
	}
	if entry != "" {
		lxc0(d)["site"] = entry
	}
	return d
}

// X25: a member's site is declared per machine, and an entry that names none takes it.
func TestX25TheMachinesSiteIsTheEntrysDefault(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("desk", "")))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if d.Site != "desk" || d.LXC[0].Site != "desk" {
		t.Fatalf("machine site %q, entry site %q; want desk twice", d.Site, d.LXC[0].Site)
	}
	if in := d.Inputs()[0]; in.LXC[0].Site != "desk" {
		t.Fatalf("the input's entry site %q, want desk", in.LXC[0].Site)
	}
}

func TestX25AnEntrysOwnSiteWins(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("desk", "lab")))
	if err != nil || d.Site != "desk" || d.LXC[0].Site != "lab" {
		t.Fatalf("machine %q, entry %q, %v; want desk and lab", d.Site, d.LXC[0].Site, err)
	}
}

func TestX25NoSiteDeclaredLeavesBothEmpty(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("", "")))
	if err != nil || d.Site != "" || d.LXC[0].Site != "" {
		t.Fatalf("machine %q, entry %q, %v; want none (the default is applied where a handle is made)", d.Site, d.LXC[0].Site, err)
	}
}

func TestX25TheMachinesSiteIsAGrammarSite(t *testing.T) {
	for name, site := range map[string]string{
		"capital": "Desk", "dotted": "host-a.example.net", "underscore": "desk_one", "empty": "", "dash": "-desk",
	} {
		d := validDecl()
		d["teamchat_site"] = site
		_, err := Decode(encode(t, d))
		wantField(t, err, "teamchat_site", site)
		if err == nil {
			t.Errorf("%s: %q was accepted", name, site)
		}
	}
	d := validDecl()
	d["teamchat_site"] = 7
	if _, err := Decode(encode(t, d)); err == nil {
		t.Error("a number was accepted as a site")
	}
}

// X26: a site that spells a name the code can see is refused, with one fixed message.
func TestX26RefusesASiteThatSpellsTheHostname(t *testing.T) {
	for name, c := range map[string]struct{ site, host string }{
		"the whole name":         {"host-a-example-net", "host-a.example.net"},
		"the first label":        {"host-a", "host-a.example.net"},
		"a middle label":         {"example", "host-a.example.net"},
		"the last label":         {"net", "host-a.example.net"},
		"a label with a capital": {"host-a", "Host-A.example.net"},
		"the name normalised":    {"member-machine-1", "Member_Machine.1"},
		"a rooted name":          {"host-a", "host-a.example.net."},
	} {
		site := c.site
		fake := &fakeHostname{name: c.host}
		d, err := Decode(encode(t, siteDecl(site, "")))
		if err != nil {
			t.Fatalf("%s: Decode: %v", name, err)
		}
		err = d.CheckSites(fake.read)
		fe, ok := errors.AsType[*FieldError](err)
		if !ok || fe.Field != "teamchat_site" || fe.Problem != SiteIsAHostname {
			t.Errorf("%s: %q: error %v, want the fixed refusal of teamchat_site", name, site, err)
		}
	}
}

func TestX26RefusesAnEntrysOwnSite(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("desk", "host-a")))
	if err != nil {
		t.Fatal(err)
	}
	fe, ok := errors.AsType[*FieldError](d.CheckSites((&fakeHostname{name: "host-a.example.net"}).read))
	if !ok || fe.Field != "teamchat_lxc_members[0].site" || fe.Problem != SiteIsAHostname {
		t.Fatalf("error %v, want the fixed refusal of the entry's site", fe)
	}
}

func TestX26RefusesALabelOfATeamsURLHost(t *testing.T) {
	for site, refused := range map[string]bool{"chat": true, "example": true, "net": true, "desk": false, "8448": false} {
		raw := siteDecl(site, "")
		team0(raw)["url"] = "https://chat.example.net:8448"
		d, err := Decode(encode(t, raw))
		if err != nil {
			t.Fatalf("%s: Decode: %v", site, err)
		}
		err = d.CheckSites((&fakeHostname{name: "member-machine-1"}).read)
		if refused != (err != nil) {
			t.Errorf("site %q: refusal %v, want %v", site, err, refused)
		}
	}
}

func TestX26AnAddressURLHostHasNoLabels(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("10", "")))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CheckSites((&fakeHostname{name: "member-machine-1"}).read); err != nil {
		t.Fatalf("a site spelling an octet of the team's address was refused: %v", err)
	}
}

func TestX26TheRefusalIsFixedAndSaysItIsBestEffort(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("host-a", "")))
	if err != nil {
		t.Fatal(err)
	}
	err = d.CheckSites((&fakeHostname{name: "host-a.example.net"}).read)
	for _, want := range []string{"hostname", "best effort", "no code reads a hostname into a handle"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %v does not say %q", err, want)
		}
	}
	if err != nil && strings.Contains(err.Error(), "example") {
		t.Errorf("the refusal echoes the hostname: %v", err)
	}
}

func TestX26TheHostnameIsAskedOnlyWhenASiteIsDeclared(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("", "")))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeHostname{err: errors.New("no hostname")}
	if err = d.CheckSites(fake.read); err != nil || fake.calls != 0 {
		t.Fatalf("no site declared: error %v after %d reads, want neither", err, fake.calls)
	}
}

func TestX26AHostnameThatCannotBeReadFailsTheCheck(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("desk", "")))
	if err != nil {
		t.Fatal(err)
	}
	for name, fake := range map[string]*fakeHostname{
		"an error": {err: errors.New("no hostname")}, "an empty name": {name: ""}, "blank": {name: "  "},
	} {
		if err = d.CheckSites(fake.read); err == nil {
			t.Errorf("%s: the check passed without a hostname", name)
		}
	}
}

func TestX26ASiteThatSpellsNothingPasses(t *testing.T) {
	d, err := Decode(encode(t, siteDecl("desk", "lab")))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.CheckSites((&fakeHostname{name: "host-a.example.net"}).read); err != nil {
		t.Fatalf("a declared site that spells no name was refused: %v", err)
	}
}
