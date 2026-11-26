package checkoutpin

import (
	"context"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// The include forms compose reads (a map's path, one or a list), and the refusals around them.
func TestComposeIncludeForms(t *testing.T) {
	dir := checkout(t)
	write(t, dir, "compose.yaml", "include:\n  - path: a.yml\n  - path: [b.yml, sub/c.yml]\n")
	for _, f := range []string{"a.yml", "b.yml", "sub/c.yml"} {
		write(t, dir, f, "services: {}\n")
	}
	files, _, _, err := ComposeDigest(dir)
	if err != nil || strings.Join(files, ",") != "compose.yaml,a.yml,b.yml,sub/c.yml" {
		t.Fatalf("files = %v, %v", files, err)
	}
	for name, content := range map[string]string{
		"missing":  "include:\n  - gone.yml\n",
		"bad yaml": "include: [\n",
		"bad item": "include:\n  - [x]\n",
		"bad path": "include:\n  - path: {a: b}\n",
	} {
		bad := checkout(t)
		write(t, bad, "compose.yml", content)
		if _, _, _, err := ComposeDigest(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// What cannot be a record or a checkout is an error, never a pass.
func TestStoreEdges(t *testing.T) {
	s, dir := store(t), checkout(t)
	if err := s.Record(dir, KindMounts, "not-a-digest", testAt); err == nil {
		t.Error("a malformed digest was recorded")
	}
	write(t, dir, "file", "x")
	if _, err := RealCheckout(dir + "/file"); err == nil {
		t.Error("a file was taken for a checkout")
	}
	if _, err := RealCheckout(dir + "/absent"); err == nil {
		t.Error("an absent directory was taken for a checkout")
	}
	if KindCompose.File() != "compose files" || !strings.Contains((&RefusalError{Kind: KindCompose, Checkout: dir, Why: "x"}).Error(), "the compose files in "+dir) {
		t.Error("the compose refusal does not name the files")
	}
	if _, err := Locate(func(string) string { return "" }); err == nil {
		t.Error("no HOME located a store")
	}
}

// The verbs' own refusals: no compose to approve, no HOME to judge mounts by, an unknown checkout.
func TestCommandEdges(t *testing.T) {
	r := newRig(t)
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"compose", r.dir}); code != term.ExitFailure {
		t.Errorf("approve compose with none: %v", code)
	}
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir}); code != term.ExitFailure {
		t.Errorf("approve mounts with none: %v", code)
	}
	if code := r.cmd().Approve(context.Background(), []string{"mounts", r.dir + "/absent"}); code != term.ExitFailure {
		t.Errorf("approve in an absent checkout: %v", code)
	}
	write(t, r.dir, Mounts, "/srv:/x\n")
	r.env["HOME"] = "relative"
	if code := r.cmd().ProjectMounts(nil); code != term.ExitFailure || !strings.Contains(r.err.String(), "HOME") {
		t.Errorf("relative HOME: %v %q", code, r.err.String())
	}
	if code := r.cmd().ProjectMounts([]string{"x"}); code != term.ExitUsage {
		t.Errorf("an argument: %v", code)
	}
	write(t, r.dir, "compose.yml", "include:\n  - ../x.yml\n")
	for _, mode := range []string{"true", "false"} {
		if code := r.cmd().Compose(context.Background(), []string{mode}); code != term.ExitFailure {
			t.Errorf("compose %s with an outside include: %v", mode, code)
		}
	}
}
