package declare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// lxcBridge is the bridge whose IPv4 network the status run adds to every team's allow_from
// (helpers/agent_bus/bridge.py:31, BRIDGE).
const lxcBridge = "lxcbr0"

// The checkout's files the verdict depends on: those the software digest is computed from
// (declare.py:461-470), the two package trees the admin bundle is built from
// (helpers/pingbus/bundle.py:39), the installer and the Tuwunel pin; and helpers/agent_bus_lxc,
// which declare.py imports (declare.py:140). The python3 that runs it is not in the key: a
// different one is an upgrade made while ccy-teams is open (B-S5).
var (
	verdictTrees = []string{"helpers/pingbus", "helpers/agent_bus", "helpers/agent_bus_lxc"}
	verdictFiles = []string{"files/usr/local/sbin/agent-bus-install", "files/usr/local/share/agent-bus/tuwunel.pin"}
)

// verdict is the last status answer kept, and the key of the inputs it was computed from.
type verdict struct {
	key    string
	states []Status
}

// Status compares each team's applied stamp in appliedDir with the declaration and the
// software in source. installed names the teams that have a homeserver unit; the flags are
// passed sorted so the call is the same whatever order a map gave them in. The answer is kept
// and given again while every input it is computed from is unchanged (SPEC-view.md V40); a
// failed run is not kept.
func (c *Client) Status(ctx context.Context, source, appliedDir string, installed []string) ([]Status, error) {
	key := c.verdictKey(source, appliedDir, installed)
	if c.verdict.states != nil && c.verdict.key == key {
		return slices.Clone(c.verdict.states), nil
	}
	c.verdict = verdict{}
	out, err := c.runFile(ctx, "status", statusArgs(source, appliedDir, installed)...)
	if err != nil {
		return nil, err
	}
	states, err := parseStatuses(out)
	if err != nil {
		return nil, err
	}
	c.verdict = verdict{key: key, states: states}
	return slices.Clone(states), nil
}

// verdictKey is a digest of every input of the status run: the arguments, the host_vars bytes,
// each applied stamp's name and bytes (declare.py:473-486), the bridge's IPv4 addresses
// (bridge.py:46-60), and the path, size, mode and modification time of each file of the
// source trees (but byte-compiled caches, which the bundle leaves out) and of the installer and
// pin. Whatever cannot be read enters the key as its error, so its appearing or going changes
// the key; the status run itself says what is wrong with it.
func (c *Client) verdictKey(source, appliedDir string, installed []string) string {
	k := &keyParts{}
	k.field("file", c.File)
	k.field("source", source)
	k.field("applied", appliedDir)
	k.field("installed", strings.Join(slices.Sorted(slices.Values(installed)), "\n"))
	k.field("host_vars", readOrError(c.File))
	stampsKey(k, appliedDir)
	k.field("bridge", c.bridgeKey())
	for _, tree := range verdictTrees {
		treeKey(k, filepath.Join(source, tree))
	}
	for _, file := range verdictFiles {
		info, err := os.Stat(filepath.Join(source, file))
		k.field("pin", file+"\n"+statOrError(info, err))
	}
	sum := sha256.Sum256([]byte(strings.Join(*k, "\n")))
	return hex.EncodeToString(sum[:])
}

// keyParts is the key's inputs in the order they were read.
type keyParts []string

// field adds one named, length-prefixed value, so no two inputs can run together.
func (k *keyParts) field(name, value string) {
	*k = append(*k, fmt.Sprintf("%s %d\n%s", name, len(value), value))
}

func readOrError(path string) string {
	body, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "error: " + err.Error()
	}
	return "bytes: " + string(body)
}

func statOrError(info fs.FileInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("%d %o %d", info.Size(), uint32(info.Mode()), info.ModTime().UnixNano())
}

// stampsKey is every <team>.json the applied directory holds, by name, with its bytes.
func stampsKey(k *keyParts, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		k.field("stamps", "error: "+err.Error())
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			k.field("stamp", e.Name()+"\n"+readOrError(filepath.Join(dir, e.Name())))
		}
	}
}

// treeKey is every file under root but those in a __pycache__ directory, by path, with its
// size, mode and modification time.
func treeKey(k *keyParts, root string) {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "__pycache__":
			return filepath.SkipDir
		case d.IsDir():
			return nil
		}
		info, err := d.Info()
		k.field("tree", path+"\n"+statOrError(info, err))
		return nil
	})
	if err != nil {
		k.field("tree", root+"\nerror: "+err.Error())
	}
}

// bridgeKey is the bridge's IPv4 addresses, or why they could not be read.
func (c *Client) bridgeKey() string {
	read := c.Bridge
	if read == nil {
		read = func() ([]string, error) { return interfaceIPv4(lxcBridge) }
	}
	addrs, err := read()
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.Join(addrs, "\n")
}

// interfaceIPv4 is the IPv4 addresses of the network interface name, in the kernel's order, as
// `ip -j -4 addr show dev <name>` lists them for v1, read in-process.
func interfaceIPv4(name string) ([]string, error) {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface %s: %w", name, err)
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, fmt.Errorf("interface %s addresses: %w", name, err)
	}
	var v4 []string
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			v4 = append(v4, ipnet.String())
		}
	}
	return v4, nil
}
