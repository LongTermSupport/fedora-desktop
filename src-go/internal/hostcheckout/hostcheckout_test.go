package hostcheckout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/gitx"
)

type fakeLxc struct {
	path  string
	err   error
	calls int
}

func (f *fakeLxc) LxcPath(context.Context) (string, error) {
	f.calls++
	return f.path, f.err
}

func installed(string) (string, error) { return "/usr/bin/lxc-config", nil }

func notInstalled(name string) (string, error) { return "", errors.New(name + " not found") }

// finder is a Finder over a git that says dir is in the work tree top (or in none when top is "").
func finder(top string, lookPath func(string) (string, error), lxc *fakeLxc) *Finder {
	fake := &execx.Fake{}
	if top == "" {
		fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{ExitCode: 128})
	} else {
		fake.On([]string{"git", "rev-parse", "--is-inside-work-tree"}, execx.Result{Stdout: []byte("true\n")})
		fake.On([]string{"git", "rev-parse", "--show-toplevel"}, execx.Result{Stdout: []byte(top + "\n")})
	}
	return &Finder{Git: gitx.New(fake), LookPath: lookPath, Lxc: lxc}
}

func TestAFolderInNoWorkTreeIsNoCheckout(t *testing.T) {
	lxc := &fakeLxc{path: "/var/lib/lxc"}
	co, err := finder("", installed, lxc).Find(context.Background(), "/somewhere")
	if err != nil || co.Top != "" || co.Host() {
		t.Fatalf("got %+v, %v", co, err)
	}
	if lxc.calls != 0 {
		t.Fatal("LXC is only asked about a checkout")
	}
}

func TestAHostCheckoutIsTheHosts(t *testing.T) {
	lxc := &fakeLxc{path: "/var/lib/lxc"}
	co, err := finder("/home/u/proj", installed, lxc).Find(context.Background(), "")
	if err != nil || co.Top != "/home/u/proj" || co.Container != "" || !co.Host() {
		t.Fatalf("got %+v, %v", co, err)
	}
}

func TestACheckoutInAContainersRootFilesystemIsTheContainers(t *testing.T) {
	lxc := &fakeLxc{path: "/var/lib/lxc"}
	co, err := finder("/var/lib/lxc/box/rootfs/srv/proj", installed, lxc).Find(context.Background(), "")
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

func TestAnythingUnderTheLxcPathIsAContainers(t *testing.T) {
	lxc := &fakeLxc{path: "/srv/lxc/"}
	co, err := finder("/srv/lxc/box/anything", installed, lxc).Find(context.Background(), "")
	if err != nil || co.Container != "box" {
		t.Fatalf("got %+v, %v", co, err)
	}
	co, err = finder("/srv/lxcother/proj", installed, lxc).Find(context.Background(), "")
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
	// git prints the resolved top; lxc-config prints the path as configured.
	co, err := finder(filepath.Join(target, "box", "rootfs", "proj"), installed, &fakeLxc{path: link}).Find(context.Background(), "")
	if err != nil || co.Container != "box" {
		t.Fatalf("got %+v, %v", co, err)
	}
}

func TestWithNoLxcInstalledNoContainerIsAsked(t *testing.T) {
	lxc := &fakeLxc{path: "/var/lib/lxc"}
	co, err := finder("/var/lib/lxc/box/rootfs/proj", notInstalled, lxc).Find(context.Background(), "")
	if err != nil || !co.Host() || lxc.calls != 0 {
		t.Fatalf("got %+v, %v, %d calls", co, err, lxc.calls)
	}
}

func TestAnLxcPathThatCannotBeReadIsAFailureNotTheHosts(t *testing.T) {
	lxc := &fakeLxc{err: errors.New("lxc-config lxc.lxcpath failed: sudo: a password is required")}
	co, err := finder("/home/u/proj", installed, lxc).Find(context.Background(), "")
	if err == nil {
		t.Fatalf("an unknown LXC path must not be taken as the host's checkout: %+v", co)
	}
	for _, want := range []string{"/home/u/proj", "LXC container", "password is required", "play-claude-yolo.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error lacks %q: %v", want, err)
		}
	}
}

func TestTheLxcPathIsAskedOnce(t *testing.T) {
	lxc := &fakeLxc{path: "/var/lib/lxc"}
	f := finder("/home/u/proj", installed, lxc)
	for range 3 {
		if _, err := f.Find(context.Background(), ""); err != nil {
			t.Fatal(err)
		}
	}
	if lxc.calls != 1 {
		t.Fatalf("asked %d times", lxc.calls)
	}
}

func TestAGitThatCannotRunIsAnError(t *testing.T) {
	f := &Finder{Git: gitx.New(&execx.Fake{}), LookPath: installed, Lxc: &fakeLxc{path: "/var/lib/lxc"}}
	if _, err := f.Find(context.Background(), ""); err == nil {
		t.Fatal("an unscripted git is an error")
	}
}
