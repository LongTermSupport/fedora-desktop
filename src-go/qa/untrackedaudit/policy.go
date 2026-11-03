package untrackedaudit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// PolicyPath is the tracked file the audit's data lives in, relative to the root.
const PolicyPath = "scripts/untracked-policy.yml"

// Policy is the audit's data: which stores the audit leaves alone, which names are scratch, how
// old is old, and which branches are being worked right now.
type Policy struct {
	// AgeHours is the floor: a file or a work tree younger than this is never reported.
	AgeHours int `yaml:"ageHours"`
	// Stores are directories directly under untracked/ that a tool owns and the audit does not
	// walk (private clones, work trees, run logs, derived facts).
	Stores []string `yaml:"stores"`
	// DaemonStores are directories directly under untracked/ that the hooks daemon owns: their
	// old files are counted as a warning, never a failure.
	DaemonStores []string `yaml:"daemonStores"`
	// AssetExtensions are the extensions of a work product: an old one is a finding even when
	// its name is a scratch shape, because a message body that is still there was never consumed.
	AssetExtensions []string `yaml:"assetExtensions"`
	// ScratchShapes are file name patterns (path.Match, against the base name) of true scratch:
	// message and body files, captures.
	ScratchShapes []string `yaml:"scratchShapes"`
	// ActiveBranches are local branches being worked right now, exempt from the unpushed check.
	ActiveBranches []string `yaml:"activeBranches"`
}

// loadPolicy reads and validates the policy; every problem is an error, so a typo in the data
// cannot quietly turn a check off.
func loadPolicy(fsys fs.FS, name string) (Policy, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return Policy{}, fmt.Errorf("cannot read %s: %w", name, err)
	}
	var p Policy
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			return Policy{}, fmt.Errorf("%s is empty", PolicyPath)
		}
		return Policy{}, fmt.Errorf("cannot parse %s: %w", PolicyPath, err)
	}
	if err := p.validate(); err != nil {
		return Policy{}, fmt.Errorf("%s: %w", PolicyPath, err)
	}
	return p, nil
}

func (p Policy) validate() error {
	if p.AgeHours <= 0 {
		return errors.New("ageHours must be a positive number")
	}
	for _, name := range slices.Concat(p.Stores, p.DaemonStores) {
		if name == "" || strings.ContainsAny(name, "/*?[") {
			return fmt.Errorf("store %q must be a plain directory name directly under untracked/", name)
		}
	}
	for _, name := range p.DaemonStores {
		if slices.Contains(p.Stores, name) {
			return fmt.Errorf("%q is both a store and a daemon store", name)
		}
	}
	if len(p.AssetExtensions) == 0 {
		return errors.New("assetExtensions is empty: nothing would be judged an asset")
	}
	for _, ext := range p.AssetExtensions {
		if !strings.HasPrefix(ext, ".") || len(ext) < 2 {
			return fmt.Errorf("asset extension %q must start with a dot", ext)
		}
	}
	for _, shape := range p.ScratchShapes {
		if _, err := path.Match(shape, "x"); err != nil {
			return fmt.Errorf("scratch shape %q: %w", shape, err)
		}
	}
	return nil
}

// maxAgeHours bounds the floor (ten years), so converting it to a Duration cannot overflow.
const maxAgeHours = 24 * 365 * 10

func (p Policy) floor() time.Duration {
	if p.AgeHours > maxAgeHours {
		return maxAgeHours * time.Hour
	}
	return time.Duration(p.AgeHours) * time.Hour
}

func (p Policy) isScratchShape(base string) bool {
	for _, shape := range p.ScratchShapes {
		if matched, err := path.Match(shape, base); err == nil && matched {
			return true
		}
	}
	return false
}

func (p Policy) isAssetExt(name string) bool {
	return slices.Contains(p.AssetExtensions, strings.ToLower(path.Ext(name)))
}
