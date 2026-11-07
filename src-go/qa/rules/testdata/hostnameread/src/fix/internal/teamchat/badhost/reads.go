package badhost

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// SiteFromEnvFile is the defect the rule exists for: an environment file's key compared with the guest's hostname
// variable, whose value then became a handle's site.
func SiteFromEnvFile(key, value string) string {
	if key == "HOOKS_DAEMON_HOSTNAME" { // want `"HOOKS_DAEMON_HOSTNAME", a place the machine's hostname is read from`
		return value
	}
	return ""
}

// CutFromLine is the same read through the prefix a line parser cuts.
func CutFromLine(line string) (string, bool) {
	return strings.CutPrefix(line, "HOOKS_DAEMON_HOSTNAME=") // want `"HOOKS_DAEMON_HOSTNAME=", a place the machine's hostname is read from`
}

// spelt is the variable's name in pieces, which is still its name.
const spelt = "HOOKS_DAEMON_" + "HOSTNAME" // want `"HOOKS_DAEMON_HOSTNAME", a place the machine's hostname is read from`

// FromEnvironment reads the variables that carry the hostname.
func FromEnvironment() (string, string, string) {
	return os.Getenv(spelt), os.Getenv("HOSTNAME"), os.Getenv("HOOKS_DAEMON_HOSTNAME") // want `"HOSTNAME", a place` `"HOOKS_DAEMON_HOSTNAME", a place`
}

// FromFiles looks at the files the system and the kernel keep it in, one under a root.
func FromFiles(root string) ([]int64, error) {
	var out []int64
	for _, name := range []string{
		"/etc/hostname",             // want `"/etc/hostname", a place the machine's hostname is read from`
		"/proc/sys/kernel/hostname", // want `"/proc/sys/kernel/hostname", a place the machine's hostname is read from`
		root + "/etc/hostname",      // want `"/etc/hostname", a place the machine's hostname is read from`
	} {
		info, err := os.Stat(name)
		if err != nil {
			return nil, err
		}
		out = append(out, info.Size())
	}
	return out, nil
}

// FromKernel asks the kernel for the node name, through both packages that wrap uname(2).
func FromKernel() error {
	var u syscall.Utsname
	if err := syscall.Uname(&u); err != nil { // want `syscall.Uname read outside teamchat/declare/machinehost.go`
		return err
	}
	var x unix.Utsname
	ask := unix.Uname // want `unix.Uname read outside teamchat/declare/machinehost.go`
	return ask(&x)
}
