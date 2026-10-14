package businstall

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

const (
	prog    = "agent-bus-install"
	svcName = "agent-bus"

	productionPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin"
	productionTool = "/usr/local/bin/agent-bus"
)

// Config is everything the installer takes from outside itself: where the host's paths are,
// who owns them, and every seam a test replaces.
type Config struct {
	// Prefix roots every path under it (a test host); "" is the real host, where the caller
	// must be root.
	Prefix string
	// AdminTool is the agent-bus admin wrapper, by path or by name on PATH.
	AdminTool string
	// Path, when not empty, is set as PATH before any program is found.
	Path string
	// Euid is the caller's effective user id.
	Euid func() int
	// RootOwner owns what root owns; SvcOwner what the agent-bus user owns. SvcOwner is
	// called only once the account exists.
	RootOwner func() (fsx.Owner, error)
	SvcOwner  func() (fsx.Owner, error)
	// Executable is the path of the running installer, which `software` installs.
	Executable func() (string, error)
	Runner     execx.Runner
	Stdout     io.Writer
	Stderr     io.Writer
	Sleep      func(time.Duration)
	// Rand is the source of the shared secret.
	Rand io.Reader
	// Pid names the temporary files.
	Pid int
	// SourceDigest identifies the installer's source: stamped with every applied team, so
	// declare can tell an out-of-date team from the source it would install now.
	SourceDigest string
}

// Production is the configuration of the real host: root only, a reset PATH, the admin
// tool at its installed path and the account lookups of the real system.
func Production(stdout, stderr io.Writer, sourceDigest string) Config {
	return Config{
		AdminTool:    productionTool,
		Path:         productionPath,
		Euid:         os.Geteuid,
		RootOwner:    func() (fsx.Owner, error) { return fsx.LookupOwner("root", "root") },
		SvcOwner:     func() (fsx.Owner, error) { return fsx.LookupOwner(svcName, svcName) },
		Executable:   os.Executable,
		Runner:       execx.OS{},
		Stdout:       stdout,
		Stderr:       stderr,
		Sleep:        time.Sleep,
		Rand:         rand.Reader,
		Pid:          os.Getpid(),
		SourceDigest: sourceDigest,
	}
}

// Test is the configuration of a test host under prefix: every path is rooted there, the
// root check is skipped, PATH is kept so stubs answer for the system tools, the admin tool is
// found on PATH and every owner is the running user. Only the test build of the binary calls
// it; the production binary never reads the variable that carries prefix.
func Test(prefix string, stdout, stderr io.Writer, sourceDigest string) (Config, error) {
	info, err := os.Stat(prefix)
	if !filepath.IsAbs(prefix) || err != nil || !info.IsDir() {
		return Config{}, errors.New("AGENT_BUS_INSTALL_TEST_PREFIX must be an existing absolute directory")
	}
	cfg := Production(stdout, stderr, sourceDigest)
	cfg.Prefix = prefix
	cfg.AdminTool = svcName
	cfg.Path = ""
	cfg.RootOwner = fsx.CurrentOwner
	cfg.SvcOwner = fsx.CurrentOwner
	return cfg, nil
}
