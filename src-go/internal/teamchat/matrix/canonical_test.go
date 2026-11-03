package matrix

import "testing"

// X9: the canonical URL two bundles are compared by is the one memberBase gives (host lower-cased,
// scheme lower-cased, port as given), judged with the CA the scheme needs: https is judged with
// one, http without, so a URL no bundle could carry is refused.
func TestCanonicalMemberBase(t *testing.T) {
	for raw, want := range map[string]string{
		"https://Matrix.Example.ORG:8448": "https://matrix.example.org:8448",
		"HTTPS://example.org":             "https://example.org",
		"https://192.0.2.7:8448":          "https://192.0.2.7:8448",
		"https://[2001:db8::1]:8448":      "https://[2001:db8::1]:8448",
		"https://[2001:DB8::1]:8448":      "https://[2001:db8::1]:8448",
		"http://127.0.0.1:8008":           "http://127.0.0.1:8008",
		"http://[::1]:8008":               "http://[::1]:8008",
		"HTTP://127.0.0.1":                "http://127.0.0.1",
	} {
		got, err := CanonicalMemberBase(raw)
		if err != nil || got != want {
			t.Errorf("CanonicalMemberBase(%q) = %q, %v; want %q", raw, got, err, want)
		}
		if twice, err := CanonicalMemberBase(got); err != nil || twice != got {
			t.Errorf("CanonicalMemberBase(%q) = %q, %v; the normal form is not its own", got, twice, err)
		}
	}
	for _, raw := range []string{
		"", "ftp://example.org", "http://example.org", "http://192.0.2.7:8008", "http://localhost:8008",
		"https://example.org/", "https://example.org/_matrix", "https://u:p@example.org", "https://example.org:0",
		"https://example.org:080", "https://ex_ample.org", "mailto:someone",
	} {
		if got, err := CanonicalMemberBase(raw); err == nil {
			t.Errorf("CanonicalMemberBase(%q) = %q, want a refusal", raw, got)
		}
	}
}
