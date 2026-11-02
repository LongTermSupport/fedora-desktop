package vmtest

// guestArch is what a guest of one CPU architecture needs from the lab: Fedora's name for the
// architecture (in tree, image and CHECKSUM paths) and the serial device its kernel's console
// is on, which the installer's log and the LUKS prompt are read from.
type guestArch struct{ Fedora, Console string }

// guestArches is every architecture the lab has guests for, keyed by GOARCH. A guest is the
// host's architecture, so KVM runs it; virt-install picks the machine type (q35 on x86_64,
// virt on aarch64) and the UEFI firmware (edk2-ovmf, edk2-aarch64) for it.
var guestArches = map[string]guestArch{
	"amd64": {Fedora: "x86_64", Console: "ttyS0"},
	"arm64": {Fedora: "aarch64", Console: "ttyAMA0"},
}

// guestArch is the host's architecture as the lab's guests need it; any other architecture fails
// naming it.
func (c *cli) guestArch() (guestArch, error) {
	a, ok := guestArches[c.env.GOARCH]
	if !ok {
		return guestArch{}, failf("the VM lab has no guest for this host's architecture: %s (it has x86_64 and aarch64)", c.env.GOARCH)
	}
	return a, nil
}
