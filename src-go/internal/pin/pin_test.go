package pin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	hashX = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func valid() string {
	return "# the pinned Tuwunel release\n" +
		"tuwunel_version: v1.2.3\n" +
		"tuwunel_sha256_x86_64: " + hashX + "\n" +
		"tuwunel_sha256_aarch64: " + hashA + "\n"
}

func TestParseValid(t *testing.T) {
	p, err := Parse("tuwunel.pin", []byte(valid()))
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v1.2.3" || p.SHA256X8664 != hashX || p.SHA256Aarch64 != hashA {
		t.Fatalf("got %+v", p)
	}
	if p.Bare() != "1.2.3" {
		t.Fatalf("Bare = %q", p.Bare())
	}
}

func TestParseTolerates(t *testing.T) {
	cases := map[string]string{
		"no trailing newline":   strings.TrimSuffix(valid(), "\n"),
		"blank lines":           "\n\n" + valid() + "\n",
		"extra spaces":          "tuwunel_version:    v1.2.3   \ntuwunel_sha256_x86_64:" + hashX + "\ntuwunel_sha256_aarch64: " + hashA + "\n",
		"comment between":       "tuwunel_version: v1.2.3\n# note\ntuwunel_sha256_x86_64: " + hashX + "\ntuwunel_sha256_aarch64: " + hashA + "\n",
		"order does not matter": "tuwunel_sha256_aarch64: " + hashA + "\ntuwunel_sha256_x86_64: " + hashX + "\ntuwunel_version: v10.20.30\n",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse("f", []byte(text)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Parity with the bash reader: the later of two equal keys wins.
func TestParseDuplicateKeyLastWins(t *testing.T) {
	text := valid() + "tuwunel_version: v9.9.9\n"
	p, err := Parse("f", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v9.9.9" {
		t.Fatalf("Version = %q", p.Version)
	}
}

func TestParseRefuses(t *testing.T) {
	cases := []struct{ name, text, msg string }{
		{"empty file", "", "tuwunel_version is missing"},
		{"missing version", "tuwunel_sha256_x86_64: " + hashX + "\ntuwunel_sha256_aarch64: " + hashA + "\n", "tuwunel_version is missing"},
		{"missing x86 hash", "tuwunel_version: v1.2.3\ntuwunel_sha256_aarch64: " + hashA + "\n", "tuwunel_sha256_x86_64 is missing"},
		{"missing aarch64 hash", "tuwunel_version: v1.2.3\ntuwunel_sha256_x86_64: " + hashX + "\n", "tuwunel_sha256_aarch64 is missing"},
		{"unknown key", valid() + "tuwunel_extra: x\n", "unknown key tuwunel_extra"},
		{"unknown key shaped like a hash key", valid() + "tuwunel_sha256_riscv: " + hashX + "\n", "unknown key tuwunel_sha256_riscv"},
		{"version without v", "tuwunel_version: 1.2.3\n", "tuwunel_version has a malformed value"},
		{"version two parts", "tuwunel_version: v1.2\n", "tuwunel_version has a malformed value"},
		{"version four parts", "tuwunel_version: v1.2.3.4\n", "tuwunel_version has a malformed value"},
		{"version prerelease", "tuwunel_version: v1.2.3-rc1\n", "tuwunel_version has a malformed value"},
		{"version with path", "tuwunel_version: v1.2.3/../../x\n", "tuwunel_version has a malformed value"},
		{"version with query", "tuwunel_version: v1.2.3?x=1\n", "tuwunel_version has a malformed value"},
		{"version signed", "tuwunel_version: v+1.2.3\n", "tuwunel_version has a malformed value"},
		{"version unicode digit", "tuwunel_version: v١.2.3\n", "tuwunel_version has a malformed value"},
		{"hash short", "tuwunel_sha256_x86_64: " + hashX[:63] + "\n", "tuwunel_sha256_x86_64 has a malformed value"},
		{"hash long", "tuwunel_sha256_x86_64: " + hashX + "a\n", "tuwunel_sha256_x86_64 has a malformed value"},
		{"hash upper", "tuwunel_sha256_aarch64: " + strings.ToUpper(hashA) + "\n", "tuwunel_sha256_aarch64 has a malformed value"},
		{"hash non-hex", "tuwunel_sha256_aarch64: " + strings.Repeat("g", 64) + "\n", "tuwunel_sha256_aarch64 has a malformed value"},
		{"hash with sha256 prefix", "tuwunel_sha256_x86_64: sha256:" + hashX + "\n", "tuwunel_sha256_x86_64 has a malformed value"},
		{"no colon", "tuwunel_version v1.2.3\n", "unreadable line"},
		{"no value", "tuwunel_version:\n", "unreadable line"},
		{"value with space inside", "tuwunel_version: v1.2.3 extra\n", "unreadable line"},
		{"leading space", " tuwunel_version: v1.2.3\n", "unreadable line"},
		{"indented comment", "  # hi\n", "unreadable line"},
		{"whitespace only line", "   \n", "unreadable line"},
		{"upper-case key", "TUWUNEL_VERSION: v1.2.3\n", "unreadable line"},
		{"carriage return", "tuwunel_version: v1.2.3\r\n", "tuwunel_version has a malformed value"},
		{"tab separated value", "tuwunel_version:\tv1.2.3\n", "tuwunel_version has a malformed value"},
		{"nul in value", "tuwunel_version: v1.2.3\x00\n", "tuwunel_version has a malformed value"},
		{"yaml list", "- tuwunel_version: v1.2.3\n", "unreadable line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("/x/tuwunel.pin", []byte(tc.text))
			if err == nil {
				t.Fatal("accepted")
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error %v is not ErrInvalid (exit 78 class)", err)
			}
			if !strings.HasPrefix(err.Error(), "/x/tuwunel.pin: ") {
				t.Fatalf("message %q does not lead with the file name", err)
			}
			if !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("message %q lacks %q", err, tc.msg)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tuwunel.pin")
	if err := os.WriteFile(file, []byte(valid()), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(file)
	if err != nil || p.Version != "v1.2.3" {
		t.Fatalf("Load = %+v, %v", p, err)
	}
	missing := filepath.Join(dir, "absent.pin")
	_, err = Load(missing)
	if !errors.Is(err, ErrInvalid) || err.Error() != missing+" is missing" {
		t.Fatalf("Load(missing) = %v", err)
	}
	// A directory in place of the file is unreadable, not a pin.
	if _, err := Load(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Load(dir) = %v", err)
	}
}

func TestBuildForMachine(t *testing.T) {
	p, err := Parse("f", []byte(valid()))
	if err != nil {
		t.Fatal(err)
	}
	x, err := p.ForMachine("x86_64")
	if err != nil {
		t.Fatal(err)
	}
	if x.Arch != "x86_64-v1" || x.SHA256 != hashX {
		t.Fatalf("x86_64: %+v", x)
	}
	if x.Asset != "v1.2.3-release-all-x86_64-v1-linux-gnu-tuwunel.zst" {
		t.Fatalf("Asset = %q", x.Asset)
	}
	if x.URL != "https://github.com/matrix-construct/tuwunel/releases/download/v1.2.3/"+x.Asset {
		t.Fatalf("URL = %q", x.URL)
	}
	if x.Binary != "tuwunel-1.2.3" {
		t.Fatalf("Binary = %q", x.Binary)
	}
	a, err := p.ForMachine("aarch64")
	if err != nil {
		t.Fatal(err)
	}
	if a.Arch != "aarch64-v8" || a.SHA256 != hashA {
		t.Fatalf("aarch64: %+v", a)
	}
	for _, m := range []string{"", "i686", "armv7l", "X86_64", "x86_64\n", "x86_64 ", "ppc64le", "../x86_64"} {
		_, err := p.ForMachine(m)
		if err == nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("ForMachine(%q) = %v, want ErrInvalid", m, err)
		}
		if !strings.Contains(err.Error(), "no pinned Tuwunel build for architecture") {
			t.Fatalf("message %q", err)
		}
	}
}
