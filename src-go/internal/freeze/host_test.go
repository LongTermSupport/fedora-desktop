package freeze

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// hostEnv supplies only the two marker paths and the container variable, so all three signals
// are exercised wherever this runs; driving it by the machine's own markers would exercise
// whichever signal that machine happens to show.
func hostEnv(t *testing.T, containerEnv, dockerEnv, containerVar string) (Env, Config) {
	t.Helper()
	return Env{
		Stat:             os.Stat,
		Getenv:           func(k string) string { return map[string]string{"container": containerVar}[k] },
		ContainerEnvPath: containerEnv,
		DockerEnvPath:    dockerEnv,
	}, testConfig(vocabs[0])
}

func TestAssertOnHostRefusesEverySignal(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present")
	absent := filepath.Join(dir, "absent")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("a podman container marker is refused", func(t *testing.T) {
		env, cfg := hostEnv(t, present, absent, "")
		err := AssertOnHost(cfg, env)
		msg := dieMsg(err)
		if msg == "" {
			t.Fatalf("allowed it: %v", err)
		}
		mustContain(t, "it names the tool", msg, "testfreeze")
		mustContain(t, "and gives the engine's own reason", msg, "not reachable from in here")
		mustEq(t, "exact", msg, "this is a container — run testfreeze on the HOST.\n  the engine is not reachable from in here.")
	})
	t.Run("a docker container marker is refused", func(t *testing.T) {
		env, cfg := hostEnv(t, absent, present, "")
		if dieMsg(AssertOnHost(cfg, env)) == "" {
			t.Fatal("allowed it")
		}
	})
	t.Run("the container environment variable is refused", func(t *testing.T) {
		env, cfg := hostEnv(t, absent, absent, "lxc")
		if dieMsg(AssertOnHost(cfg, env)) == "" {
			t.Fatal("allowed it")
		}
	})
	// The direction the old shape could never assert: with no signal at all the guard must get
	// out of the way.
	t.Run("with no container signal at all, a host is allowed", func(t *testing.T) {
		env, cfg := hostEnv(t, absent, absent, "")
		if err := AssertOnHost(cfg, env); err != nil {
			t.Fatalf("refused a machine showing no marker: %v", err)
		}
	})
}

// Every case above supplies both paths, so none evaluates the defaults, and a typo in either
// default would ship green while the guard silently stopped refusing.
func TestDefaultMarkerPathsAreTheRealOnes(t *testing.T) {
	t.Run("the podman marker default is the real path", func(t *testing.T) {
		mustEq(t, "const", ContainerEnvPath, "/run/.containerenv")
	})
	t.Run("the docker marker default is the real path", func(t *testing.T) {
		mustEq(t, "const", DockerEnvPath, "/.dockerenv")
	})
	t.Run("NewEnv wires the real paths, and nothing loosens them", func(t *testing.T) {
		env := NewEnv(nil, nil, nil)
		mustEq(t, "container", env.ContainerEnvPath, ContainerEnvPath)
		mustEq(t, "docker", env.DockerEnvPath, DockerEnvPath)
	})
}

// A marker whose stat fails for a reason other than "not there" leaves the guard unable to say
// it is on a host; it fails rather than guessing.
func TestAssertOnHostFailsWhenItCannotTell(t *testing.T) {
	boom := errors.New("input/output error")
	env := Env{
		Stat:   func(string) (os.FileInfo, error) { return nil, boom },
		Getenv: func(string) string { return "" },
	}
	err := AssertOnHost(testConfig(vocabs[0]), env)
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

// A directory of the marker's name is not the marker file (the bash test was -f).
func TestAssertOnHostIgnoresADirectoryOfThatName(t *testing.T) {
	dir := t.TempDir()
	env, cfg := hostEnv(t, dir, filepath.Join(dir, "absent"), "")
	if err := AssertOnHost(cfg, env); err != nil {
		t.Fatalf("a directory is not the marker file: %v", err)
	}
}

func TestNewEnvFillsRealSeams(t *testing.T) {
	env := NewEnv(nil, nil, nil)
	if env.Getenv == nil || env.Stat == nil || env.LookPath == nil || env.OpenTTY == nil || env.IsTerminal == nil {
		t.Fatalf("a seam is nil: %+v", env)
	}
	if env.Getenv("PATH") == "" {
		t.Error("Getenv reads the real environment")
	}
	if _, err := env.Stat("/"); err != nil {
		t.Error(err)
	}
	if env.IsTerminal("not a file") {
		t.Error("a string is not a terminal")
	}
	if _, err := env.LookPath("freeze-test-no-such-tool"); err == nil {
		t.Error("LookPath finds nothing for an absent tool")
	}
	if tty, err := env.OpenTTY(); err == nil {
		if cerr := tty.Close(); cerr != nil {
			t.Error(cerr)
		}
	}
}
