package vmtest

import (
	"context"
	"net/netip"
	"regexp"
	"strings"
)

// labNetworkRE is a lab network's name, the key of the host_vars mapping vm_test_lab_networks.
var labNetworkRE = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// labNetworkPrefix starts each lab.env key the lab play writes from vm_test_lab_networks.
const labNetworkPrefix = "VMTEST_NET_"

// cmdLabNetwork prints the subnet the owner gave a lab network (E74): the lab sets no address
// itself, and a scenario reads its subnets here rather than from the lab's layout.
func (c *cli) cmdLabNetwork(_ context.Context, args []string) error {
	if len(args) != 1 {
		return usagef("usage: vmtest lab-network <name>")
	}
	name := args[0]
	if !labNetworkRE.MatchString(name) {
		return usagef("a lab network name is 1 to 16 of a-z and 0-9, starting with a letter; got '%s'", name)
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	key := labNetworkPrefix + strings.ToUpper(name)
	value, ok := l.vars[key]
	if !ok {
		return failf("lab.env sets no %s; set vm_test_lab_networks.%s in host_vars and re-run %s", key, name, playName)
	}
	prefix, perr := netip.ParsePrefix(value)
	if perr != nil || !prefix.Addr().Is4() || prefix.Masked() != prefix {
		return failf("%s is '%s', not a canonical IPv4 CIDR; re-run %s", key, value, playName)
	}
	c.sayf("%s", prefix.String())
	return nil
}
