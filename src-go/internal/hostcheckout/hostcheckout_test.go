package hostcheckout

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

// conf is /etc/lxc/lxc.conf as a test gives it: absent when absent is set, else text, or err.
type conf struct {
	text   string
	absent bool
	err    error
	reads  int
}

func (c *conf) read(path string) ([]byte, error) {
	c.reads++
	if path != LxcConf {
		return nil, errors.New("read of " + path)
	}
	switch {
	case c.err != nil:
		return nil, c.err
	case c.absent:
		return nil, fs.ErrNotExist
	}
	return []byte(c.text), nil
}

// finder is a Finder over a git that says dir is in the work tree top (or in none when top is "").
func finder(top string, c *conf) *Finder {
	fake := &execx.Fake{}
	if top == "" {
		fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{ExitCode: 128})
	} else {
		fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{Stdout: []byte("true\n")})
		fake.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{Stdout: []byte(top + "\n")})
	}
	return &Finder{Git: gitx.New(fake), ReadFile: c.read}
}

func noConf() *conf { return &conf{absent: true} }

func TestAFolderInNoWorkTreeIsNoCheckout(t *testing.T) {
	c := noConf()
	co, err := finder("", c).Find(context.Background(), "/somewhere")
	if err != nil || co.Top != "" || co.Host() {
		t.Fatalf("got %+v, %v", co, err)
	}
	if c.reads != 0 {
		t.Fatal("the LXC path is only read for a checkout")
	}
}

func TestAHostCheckoutIsTheHosts(t *testing.T) {
	co, err := finder("/home/u/proj", noConf()).Find(context.Background(), "")
	if err != nil || co.Top != "/home/u/proj" || co.Container != "" || !co.Host() {
		t.Fatalf("got %+v, %v", co, err)
	}
}

func TestWithNoLxcConfTheDefaultPathIsTheContainers(t *testing.T) {
	co, err := finder("/var/lib/lxc/box/rootfs/srv/proj", noConf()).Find(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if co.Container != "box" || co.Host() {
		t.Fatalf("got %+v", co)
	}
	why := co.Refusal("/var/lib/lxc/box/rootfs/srv/proj")
	for _, want := range []string{
		"LXC container box's checkout", "/var/lib/lxc/box/rootfs/srv/proj",
		"host or podman session", "ccy-teams lxc-add", "inside the container",
	} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal lacks %q: %s", want, why)
		}
	}
}

func TestTheLxcPathIsReadFromLxcConf(t *testing.T) {
	cases := map[string]string{
		"a custom path":            "lxc.lxcpath = /srv/lxc\n",
		"no spaces":                "lxc.lxcpath=/srv/lxc",
		"whitespace around it all": "  \t lxc.lxcpath \t=\t /srv/lxc/  \t\n",
		"after comments and other keys": "# lxc.lxcpath = /var/lib/lxc\n\n" +
			"lxc.default_config = /etc/lxc/default.conf\n   # indented comment\nlxc.lxcpath = /srv/lxc\n",
	}
	for name, text := range cases {
		f := finder("/srv/lxc/box/rootfs/proj", &conf{text: text})
		co, err := f.Find(context.Background(), "")
		if err != nil || co.Container != "box" {
			t.Errorf("%s: got %+v, %v", name, co, err)
		}
		f = finder("/var/lib/lxc/box/rootfs/proj", &conf{text: text})
		if co, err := f.Find(context.Background(), ""); err != nil || !co.Host() {
			t.Errorf("%s: the default path is not LXC's once another is set: %+v, %v", name, co, err)
		}
	}
}

func TestACommentedLxcPathLeavesTheDefault(t *testing.T) {
	co, err := finder("/var/lib/lxc/box/rootfs/proj", &conf{text: "# lxc.lxcpath = /srv/lxc\n"}).Find(context.Background(), "")
	if err != nil || co.Container != "box" {
		t.Fatalf("got %+v, %v", co, err)
	}
}

func TestAnLxcConfThatCannotBeReadOrParsedIsAFailureNotTheHosts(t *testing.T) {
	cases := map[string]*conf{
		"unreadable":       {err: fs.ErrPermission},
		"a line with no =": {text: "lxc.lxcpath /srv/lxc\n"},
		"an empty path":    {text: "lxc.lxcpath =\n"},
		"a relative path":  {text: "lxc.lxcpath = lxc\n"},
	}
	for name, c := range cases {
		co, err := finder("/home/u/proj", c).Find(context.Background(), "")
		if err == nil {
			t.Errorf("%s: an unknown LXC path must not be taken as the host's checkout: %+v", name, co)
			continue
		}
		for _, want := range []string{"/home/u/proj", "LXC container", LxcConf} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the error lacks %q: %v", name, want, err)
			}
		}
	}
}

func TestAnythingUnderTheLxcPathIsAContainers(t *testing.T) {
	c := &conf{text: "lxc.lxcpath = /srv/lxc/\n"}
	co, err := finder("/srv/lxc/box/anything", c).Find(context.Background(), "")
	if err != nil || co.Container != "box" {
		t.Fatalf("got %+v, %v", co, err)
	}
	co, err = finder("/srv/lxcother/proj", c).Find(context.Background(), "")
	if err != nil || !co.Host() {
		t.Fatalf("a sibling folder that only starts with the same letters is the host's: %+v, %v", co, err)
	}
}

func TestTheLxcPathIsMatchedThroughALink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "data", "lxc")
	if err := os.MkdirAll(filepath.Join(target, "box", "rootfs"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "lxc")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	// git prints the resolved top; lxc.conf names the path as configured.
	c := &conf{text: "lxc.lxcpath = " + link + "\n"}
	co, err := finder(filepath.Join(target, "box", "rootfs", "proj"), c).Find(context.Background(), "")
	if err != nil || co.Container != "box" {
		t.Fatalf("got %+v, %v", co, err)
	}
}

func TestTheLxcPathIsReadOnce(t *testing.T) {
	c := noConf()
	f := finder("/home/u/proj", c)
	for range 3 {
		if _, err := f.Find(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
	if c.reads != 1 {
		t.Fatalf("read %d times", c.reads)
	}
}

func TestAGitThatCannotRunIsAnError(t *testing.T) {
	f := &Finder{Git: gitx.New(&execx.Fake{}), ReadFile: noConf().read}
	if _, err := f.Find(context.Background(), ""); err == nil {
		t.Fatal("an unscripted git is an error")
	}
}
