package engine

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func engineOn(kind Kind) (Engine, *execx.Fake) {
	f := &execx.Fake{}
	return Engine{Kind: kind, Runner: f}, f
}

func TestImageExists(t *testing.T) {
	cases := []struct {
		name   string
		kind   Kind
		result execx.Result
		want   bool
	}{
		{"podman has it", Podman, execx.Result{Stdout: []byte("sha256:abc\n")}, true},
		{"podman does not know it", Podman, execx.Result{ExitCode: 125, Stderr: []byte("Error: claude-yolo:latest: image not known\n")}, false},
		{"docker has no such image", Docker, execx.Result{ExitCode: 1, Stderr: []byte("Error: No such image: claude-yolo:latest\n")}, false},
		{"an older docker has no such object", Docker, execx.Result{ExitCode: 1, Stderr: []byte("Error: No such object: claude-yolo:latest\n")}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, f := engineOn(c.kind)
			f.On([]string{string(c.kind), "image", "inspect"}, c.result)
			got, err := e.ImageExists(context.Background(), "claude-yolo:latest")
			if err != nil {
				t.Fatalf("ImageExists: %v", err)
			}
			if got != c.want {
				t.Fatalf("ImageExists = %v, want %v", got, c.want)
			}
			want := []string{"image", "inspect", "--format", "{{.Id}}", "claude-yolo:latest"}
			if call := f.Calls()[0]; call.Name != string(c.kind) || !slices.Equal(call.Args, want) {
				t.Fatalf("ran %q, want %s %q", call.Line(), c.kind, want)
			}
		})
	}
}

// The bash launcher read every failed inspect as "no image" and went on to build one, so an
// engine that was not running read as a missing image.
func TestImageExistsReportsAnEngineThatFailed(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: cannot connect to Podman\n")})
	if _, err := e.ImageExists(context.Background(), "claude-yolo:latest"); err == nil {
		t.Fatal("an engine that could not answer was read as an answer")
	}
	f.OnError([]string{"podman", "image", "inspect"}, &execx.NotInstalledError{Name: "podman"})
	if _, err := e.ImageExists(context.Background(), "claude-yolo:latest"); !execx.IsNotInstalled(err) {
		t.Fatalf("ImageExists error = %v, want not installed", err)
	}
}

func TestImageInspectReturnsTheFormattedField(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "image", "inspect", "--format", "{{.Created}}", "x"}, execx.Result{Stdout: []byte("2026-01-01\n")})
	got, err := e.ImageInspect(context.Background(), "x", "{{.Created}}")
	if err != nil || got != "2026-01-01" {
		t.Fatalf("ImageInspect = %q, %v", got, err)
	}
	f.On([]string{"podman", "image", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("Error: x: image not known")})
	if _, err := e.ImageInspect(context.Background(), "x", "{{.Created}}"); !errors.Is(err, ErrNoImage) {
		t.Fatalf("ImageInspect error = %v, want ErrNoImage", err)
	}
}

func TestContainers(t *testing.T) {
	e, f := engineOn(Docker)
	f.On([]string{"docker", "ps"}, execx.Result{Stdout: []byte("proj_ccy_1\n\nproj_ccy_2\n")})
	got, err := e.Containers(context.Background(), ContainerQuery{All: true, Filters: []string{"name=_ccy"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"proj_ccy_1", "proj_ccy_2"}; !slices.Equal(got, want) {
		t.Fatalf("Containers = %q, want %q", got, want)
	}
	want := []string{"ps", "-a", "--filter", "name=_ccy", "--format", "{{.Names}}"}
	if args := f.Calls()[0].Args; !slices.Equal(args, want) {
		t.Fatalf("ps args = %q, want %q", args, want)
	}
	f.On([]string{"docker", "ps"}, execx.Result{ExitCode: 1, Stderr: []byte("Cannot connect")})
	if _, err := e.Containers(context.Background(), ContainerQuery{}); err == nil {
		t.Fatal("a failed ps was read as no containers")
	}
}

func TestContainersWithNoneRunningIsEmpty(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "ps"}, execx.Result{})
	got, err := e.Containers(context.Background(), ContainerQuery{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Containers = %q, %v; want none", got, err)
	}
	if want := []string{"ps", "--format", "{{.Names}}"}; !slices.Equal(f.Calls()[0].Args, want) {
		t.Fatalf("ps args = %q, want %q", f.Calls()[0].Args, want)
	}
}

func TestRemove(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "rm", "-f", "proj_ccy_1"}, execx.Result{Stdout: []byte("proj_ccy_1\n")})
	if err := e.Remove(context.Background(), "proj_ccy_1"); err != nil {
		t.Fatal(err)
	}
	f.On([]string{"podman", "rm"}, execx.Result{ExitCode: 2, Stderr: []byte("Error: busy")})
	err := e.Remove(context.Background(), "proj_ccy_1")
	if exit, ok := errors.AsType[*execx.ExitError](err); !ok || exit.Code != 2 {
		t.Fatalf("Remove error = %v, want the engine's exit 2", err)
	}
}

func TestNetworks(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "network", "ls", "--format", "{{.Name}}"}, execx.Result{Stdout: []byte("podman\nproj_default\n")})
	got, err := e.Networks(context.Background())
	if err != nil || !slices.Equal(got, []string{"podman", "proj_default"}) {
		t.Fatalf("Networks = %q, %v", got, err)
	}
	f.On([]string{"podman", "network"}, execx.Result{ExitCode: 125})
	if _, err := e.Networks(context.Background()); err == nil {
		t.Fatal("a failed network ls was read as no networks")
	}
}

func TestRootless(t *testing.T) {
	cases := []struct {
		name       string
		kind       Kind
		format     string
		result     execx.Result
		want       Posture
		wantReport string
		wantCode   int
	}{
		{"podman rootless", Podman, "{{.Host.Security.Rootless}}", execx.Result{Stdout: []byte("true\n")}, PostureRootless, "true\n", 0},
		{"podman rootful", Podman, "{{.Host.Security.Rootless}}", execx.Result{Stdout: []byte("false\n")}, PostureRootful, "false\n", 0},
		{"docker rootless", Docker, "{{.SecurityOptions}}", execx.Result{Stdout: []byte("[name=rootless]\n")}, PostureRootless, "[name=rootless]\n", 0},
		{
			"the error text is the report, and the report is unknown", Podman, "{{.Host.Security.Rootless}}",
			execx.Result{ExitCode: 125, Stderr: []byte("Error: unable to connect\n")},
			PostureUnknown, "Error: unable to connect\n", 125,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, f := engineOn(c.kind)
			f.On([]string{string(c.kind), "info", "--format", c.format}, c.result)
			got, err := e.Rootless(context.Background())
			if err != nil {
				t.Fatalf("Rootless: %v", err)
			}
			want := Report{Posture: c.want, Command: string(c.kind) + " info --format '" + c.format + "'", Output: c.wantReport, ExitCode: c.wantCode}
			if got != want {
				t.Fatalf("Rootless = %+v, want %+v", got, want)
			}
		})
	}
}

func TestRootlessOfAnEngineThatCannotStart(t *testing.T) {
	e, f := engineOn(Podman)
	f.OnError([]string{"podman", "info"}, &execx.NotInstalledError{Name: "podman"})
	if _, err := e.Rootless(context.Background()); !execx.IsNotInstalled(err) {
		t.Fatalf("Rootless error = %v, want not installed", err)
	}
	if _, err := (Engine{Kind: "nerdctl", Runner: f}).Rootless(context.Background()); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("Rootless of an unknown kind = %v, want ErrUnknownKind", err)
	}
}

func TestSELinuxMode(t *testing.T) {
	cases := []struct {
		name    string
		kind    Kind
		setup   func(*execx.Fake)
		want    SELinux
		relabel string
	}{
		{"enforcing podman", Podman, func(f *execx.Fake) {
			f.On([]string{"getenforce"}, execx.Result{Stdout: []byte("Enforcing\n")})
			f.On([]string{"podman", "info", "--format", "{{.Host.Security.SELinuxEnabled}}"}, execx.Result{Stdout: []byte("true\n")})
		}, SELinuxEnforcing, "z"},
		{"no getenforce, podman not labelling", Podman, func(f *execx.Fake) {
			f.OnError([]string{"getenforce"}, &execx.NotInstalledError{Name: "getenforce"})
			f.On([]string{"podman", "info"}, execx.Result{Stdout: []byte("false\n")})
		}, SELinuxOff, ""},
		{"getenforce fails: its text is unreadable", Podman, func(f *execx.Fake) {
			f.On([]string{"getenforce"}, execx.Result{ExitCode: 1, Stderr: []byte("getenforce: broken\n")})
			f.On([]string{"podman", "info"}, execx.Result{Stdout: []byte("true\n")})
		}, SELinuxUnknown, "z"},
		{"docker labelling on an enforcing host", Docker, func(f *execx.Fake) {
			f.On([]string{"getenforce"}, execx.Result{Stdout: []byte("Enforcing\n")})
			f.On([]string{"docker", "info", "--format", "{{.SecurityOptions}}"}, execx.Result{Stdout: []byte("[name=seccomp name=selinux]\n")})
		}, SELinuxEnforcing, "z"},
		{"docker unreachable on an enforcing host relabels", Docker, func(f *execx.Fake) {
			f.On([]string{"getenforce"}, execx.Result{Stdout: []byte("Enforcing\n")})
			f.On([]string{"docker", "info"}, execx.Result{ExitCode: 1, Stderr: []byte("Cannot connect to the Docker daemon\n")})
		}, SELinuxUnknown, "z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, f := engineOn(c.kind)
			c.setup(f)
			got, err := e.SELinuxMode(context.Background())
			if err != nil {
				t.Fatalf("SELinuxMode: %v", err)
			}
			if got != c.want || got.Relabel() != c.relabel {
				t.Fatalf("SELinuxMode = %v (relabel %q), want %v (%q)", got, got.Relabel(), c.want, c.relabel)
			}
		})
	}
}

func TestSELinuxModeOfAnEngineThatCannotStart(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"getenforce"}, execx.Result{Stdout: []byte("Enforcing\n")})
	f.OnError([]string{"podman", "info"}, &execx.NotInstalledError{Name: "podman"})
	if _, err := e.SELinuxMode(context.Background()); !execx.IsNotInstalled(err) {
		t.Fatalf("SELinuxMode error = %v, want not installed", err)
	}
	g, gf := engineOn(Podman)
	gf.OnError([]string{"getenforce"}, errors.New("fork failed"))
	if _, err := g.SELinuxMode(context.Background()); err == nil {
		t.Fatal("a getenforce that could not start for another reason was read as no SELinux")
	}
	if _, err := (Engine{Kind: "nerdctl", Runner: gf}).SELinuxMode(context.Background()); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("SELinuxMode of an unknown kind = %v, want ErrUnknownKind", err)
	}
}

func TestUIDMapReadsStdoutOnly(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "unshare", "cat", "/proc/self/uid_map"}, execx.Result{
		Stdout: []byte("         0       1000          1\n"),
		Stderr: []byte("level=warning msg=\"a warning\"\n"),
	})
	ranges, err := e.UIDMap(context.Background())
	if err != nil {
		t.Fatalf("UIDMap: %v", err)
	}
	if want := []Range{{Inside: 0, Outside: 1000, Count: 1}}; !slices.Equal(ranges, want) {
		t.Fatalf("UIDMap = %+v, want %+v", ranges, want)
	}
}

func TestUIDMapRefusals(t *testing.T) {
	e, f := engineOn(Podman)
	f.On([]string{"podman", "unshare"}, execx.Result{ExitCode: 3, Stderr: []byte("Error: stub podman failed")})
	if _, err := e.UIDMap(context.Background()); err == nil {
		t.Fatal("podman could not report its uid map, and the launch went on")
	}
	f.On([]string{"podman", "unshare"}, execx.Result{Stdout: []byte("Error: not a map")})
	if _, err := e.UIDMap(context.Background()); !errors.Is(err, ErrUnreadableUIDMap) {
		t.Fatalf("UIDMap error = %v, want ErrUnreadableUIDMap", err)
	}
	docker := Engine{Kind: Docker, Runner: &execx.Fake{}}
	if _, err := docker.UIDMap(context.Background()); !errors.Is(err, ErrNoUIDMap) {
		t.Fatalf("docker UIDMap error = %v, want ErrNoUIDMap", err)
	}
}
