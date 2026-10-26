package setuprclone

import (
	"strings"
	"testing"
)

const vaultOut = "rclone_config: !vault |\n" +
	"          $ANSIBLE_VAULT;1.2;AES256;localhost\n" +
	"          3131\n"

const baseVars = `---
user_login: "u"
# a comment that stays
rclone_config: !vault |
          $ANSIBLE_VAULT;1.2;AES256;localhost
          3030

rclone_mounts:
  - name: gdrive
    remote: "gdrive:/"
    mountpoint: "/home/u/mnt/gdrive"
    rc_port: 5590
    cache_mode: writes
  - name: photos
    remote: "s3:bucket/photos"
    mountpoint: "/home/u/mnt/photos"

other_key: [1, 2]
`

func mustParse(t *testing.T, text string) hostVars {
	t.Helper()
	hv, err := parseHostVars([]byte(text))
	if err != nil {
		t.Fatalf("parseHostVars: %v", err)
	}
	return hv
}

// E14: the mounts are read in order, with their three values.
func TestParseMounts(t *testing.T) {
	hv := mustParse(t, baseVars)
	if len(hv.mounts) != 2 {
		t.Fatalf("mounts = %d, want 2", len(hv.mounts))
	}
	m := hv.mounts[0]
	if m.name != "gdrive" || m.remote != "gdrive:/" || m.mountpoint != "/home/u/mnt/gdrive" {
		t.Errorf("first mount = %+v", m)
	}
	if m.remoteName() != "gdrive" || m.folder() != "/" {
		t.Errorf("remoteName/folder = %q %q", m.remoteName(), m.folder())
	}
	if hv.mounts[1].folder() != "bucket/photos" {
		t.Errorf("folder = %q", hv.mounts[1].folder())
	}
}

// E14: absent, null and empty are no mount; an empty file is no mount.
func TestParseNoMounts(t *testing.T) {
	for _, text := range []string{"", "a: 1\n", "rclone_mounts:\n", "rclone_mounts: ~\n", "rclone_mounts: []\n"} {
		hv := mustParse(t, text)
		if len(hv.mounts) != 0 {
			t.Errorf("%q: mounts = %d", text, len(hv.mounts))
		}
	}
}

// E14: anything else is refused, naming the entry.
func TestParseRefuses(t *testing.T) {
	cases := map[string]string{
		"not yaml":        "a: [\n",
		"not a mapping":   "- a\n",
		"twice":           "a: 1\na: 2\n",
		"mounts mapping":  "rclone_mounts: {a: 1}\n",
		"entry scalar":    "rclone_mounts:\n  - x\n",
		"no name":         "rclone_mounts:\n  - remote: \"r:/\"\n    mountpoint: /m\n",
		"vaulted name":    "rclone_mounts:\n  - name: !vault x\n    remote: \"r:/\"\n    mountpoint: /m\n",
		"null mountpoint": "rclone_mounts:\n  - name: a\n    remote: \"r:/\"\n    mountpoint:\n",
		"no colon":        "rclone_mounts:\n  - name: a\n    remote: r\n    mountpoint: /m\n",
		"key twice":       "rclone_mounts:\n  - name: a\n    name: b\n    remote: \"r:/\"\n    mountpoint: /m\n",
		"alias":           "x: &a [1]\nrclone_mounts: *a\n",
	}
	for name, text := range cases {
		if _, err := parseHostVars([]byte(text)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	_, err := parseHostVars([]byte("rclone_mounts:\n  - name: a\n    remote: \"r:/\"\n    mountpoint: /m\n  - 3\n"))
	if err == nil || !strings.Contains(err.Error(), "entry 2") {
		t.Errorf("error does not name entry 2: %v", err)
	}
}

// E17: an edit keeps the other keys of the entry and its place, and touches nothing else.
func TestEditMountKeepsOtherKeys(t *testing.T) {
	hv := mustParse(t, baseVars)
	mounts := hv.mounts
	mounts[0] = mounts[0].with("gdrive:/work", "gdrive", "/home/u/mnt/work")
	out, err := editHostVars([]byte(baseVars), "", mounts)
	if err != nil {
		t.Fatalf("editHostVars: %v", err)
	}
	text := string(out)
	for _, want := range []string{"rc_port: 5590", "cache_mode: writes", "/home/u/mnt/work", "gdrive:/work", "# a comment that stays", "3030", "other_key: [1, 2]"} {
		if !strings.Contains(text, want) {
			t.Errorf("edited text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "/home/u/mnt/gdrive\"") {
		t.Errorf("old mountpoint still there:\n%s", text)
	}
	if !strings.HasPrefix(text, "---\nuser_login: \"u\"\n# a comment that stays\nrclone_config: !vault |\n") {
		t.Errorf("head of the file changed:\n%s", text)
	}
	if !strings.HasSuffix(text, "\nother_key: [1, 2]\n") {
		t.Errorf("tail of the file changed:\n%s", text)
	}
	again := mustParse(t, text)
	if again.mounts[0].mountpoint != "/home/u/mnt/work" || again.mounts[1].name != "photos" {
		t.Errorf("read back = %+v", again.mounts)
	}
}

// E17: the vault text replaces rclone_config in its place.
func TestEditReplacesVaultInPlace(t *testing.T) {
	hv := mustParse(t, baseVars)
	out, err := editHostVars([]byte(baseVars), vaultOut, hv.mounts)
	if err != nil {
		t.Fatalf("editHostVars: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "3030") || !strings.Contains(text, "3131") {
		t.Errorf("vault not replaced:\n%s", text)
	}
	if strings.Index(text, "rclone_config") > strings.Index(text, "rclone_mounts") {
		t.Errorf("rclone_config moved:\n%s", text)
	}
}

// E17: absent keys are appended after a blank line; new entries quote remote and mountpoint.
func TestEditAppends(t *testing.T) {
	text := "user_login: u\n"
	m := newMount("gdrive:/", "gdrive", "/home/u/mnt/gdrive")
	out, err := editHostVars([]byte(text), vaultOut, []mount{m})
	if err != nil {
		t.Fatalf("editHostVars: %v", err)
	}
	want := "user_login: u\n\n" + vaultOut + "\nrclone_mounts:\n  - name: gdrive\n    remote: \"gdrive:/\"\n    mountpoint: \"/home/u/mnt/gdrive\"\n"
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// E17: a file without a final newline still gets a well-formed append.
func TestEditAppendsAfterNoNewline(t *testing.T) {
	out, err := editHostVars([]byte("a: 1"), "", []mount{newMount("r:/", "r", "/m")})
	if err != nil {
		t.Fatalf("editHostVars: %v", err)
	}
	if !strings.HasPrefix(string(out), "a: 1\n\nrclone_mounts:\n") {
		t.Errorf("got:\n%s", out)
	}
}

// E17: no mount left removes the block, and its trailing blank line stays outside it.
func TestEditRemovesBlock(t *testing.T) {
	out, err := editHostVars([]byte(baseVars), "", nil)
	if err != nil {
		t.Fatalf("editHostVars: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "rclone_mounts") || strings.Contains(text, "photos") {
		t.Errorf("block not removed:\n%s", text)
	}
	if !strings.Contains(text, "3030\n\nother_key") {
		t.Errorf("spacing around the removed block:\n%s", text)
	}
}

// E18: an edit that would change another key is refused (a column-0 comment splitting a
// block leaves indented lines that belong to nothing, which the read-back catches).
func TestEditReadBackRefuses(t *testing.T) {
	text := "rclone_mounts:\n  - name: a\n    remote: \"r:/\"\n# split\n    mountpoint: /m\nb: 1\n"
	if _, err := parseHostVars([]byte(text)); err != nil {
		t.Fatalf("fixture should parse: %v", err)
	}
	_, err := editHostVars([]byte(text), "", []mount{newMount("r:/", "a", "/n")})
	if err == nil {
		t.Fatalf("no error for a block the edit cannot replace cleanly")
	}
}

// E18: the vault text must be what ansible-vault printed: one rclone_config !vault value.
func TestParseVault(t *testing.T) {
	if _, err := parseVault([]byte(vaultOut), "rclone_config"); err != nil {
		t.Errorf("good output refused: %v", err)
	}
	for _, bad := range []string{"", "rclone_config: plain\n", "other: !vault |\n  x\n", vaultOut + "extra: 1\n", "[\n"} {
		if _, err := parseVault([]byte(bad), "rclone_config"); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// E16: the mounts as read are unchanged; any edit, removal or addition is a change.
func TestMountsChanged(t *testing.T) {
	hv := mustParse(t, baseVars)
	if mountsChanged(hv.mounts, hv.mounts) {
		t.Errorf("same mounts read as changed")
	}
	if !mountsChanged(hv.mounts, hv.mounts[:1]) {
		t.Errorf("removal not a change")
	}
	edited := append([]mount(nil), hv.mounts...)
	edited[1] = edited[1].with("s3:bucket/photos", "photos", "/home/u/mnt/p2")
	if !mountsChanged(hv.mounts, edited) {
		t.Errorf("edit not a change")
	}
	if hv.mounts[1].mountpoint != "/home/u/mnt/photos" {
		t.Errorf("with changed the original mount")
	}
}
