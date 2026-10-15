package vmtestmanifest

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"github.com/LongTermSupport/fedora-desktop/src-go/qa/discovery"
)

const (
	acceptancePrefix = "guest-acceptance-"
	preparePrefix    = "guest-prepare-"
	scriptSuffix     = ".bash"
)

// scenario is the part of a manifest scenario the cross-checks read.
type scenario struct {
	id      string
	profile string
	// planned is nil for a scenario that has not declared its check count.
	planned *int
	// reboot is the raw value of reboot_before_checks, and whether the key is there at all.
	reboot    any
	hasReboot bool
}

// shape is the manifest as the cross-checks read it.
type shape struct {
	scenarios []scenario // sorted by id
	profiles  map[string]bool
}

// readShape reads the parts of a manifest the validator accepted. A manifest it accepted but this
// cannot read is a disagreement between the two, which is an error and not a finding.
func readShape(doc map[string]any) (shape, error) {
	bases, ok := doc["vm_test_bases"].(map[string]any)
	if !ok {
		return shape{}, errors.New("the validator accepted the manifest but vm_test_bases is not a mapping")
	}
	profiles := map[string]string{}
	for key, raw := range bases {
		base, isMap := raw.(map[string]any)
		profile, isString := base["profile"].(string)
		if !isMap || !isString {
			return shape{}, fmt.Errorf("the validator accepted the manifest but base %q has no profile", key)
		}
		profiles[key] = profile
	}
	scenarios, ok := doc["vm_test_scenarios"].(map[string]any)
	if !ok {
		return shape{}, errors.New("the validator accepted the manifest but vm_test_scenarios is not a mapping")
	}
	out := shape{profiles: map[string]bool{}}
	for _, profile := range profiles {
		out.profiles[profile] = true
	}
	for _, id := range slices.Sorted(maps.Keys(scenarios)) {
		sc, err := readScenario(id, scenarios[id], profiles)
		if err != nil {
			return shape{}, err
		}
		out.scenarios = append(out.scenarios, sc)
	}
	return out, nil
}

func readScenario(id string, raw any, profiles map[string]string) (scenario, error) {
	entry, ok := raw.(map[string]any)
	if !ok {
		return scenario{}, fmt.Errorf("the validator accepted the manifest but scenario %q is not a mapping", id)
	}
	baseKey, isString := entry["base"].(string)
	profile, declared := profiles[baseKey]
	if !isString || !declared {
		return scenario{}, fmt.Errorf("the validator accepted the manifest but scenario %q names base %v, which is not declared", id, entry["base"])
	}
	sc := scenario{id: id, profile: profile}
	sc.reboot, sc.hasReboot = entry["reboot_before_checks"]
	switch planned := entry["planned"].(type) {
	case nil:
	case int:
		sc.planned = &planned
	default:
		return scenario{}, fmt.Errorf("the validator accepted the manifest but scenario %q has planned %v, which is neither an integer nor null", id, planned)
	}
	return sc, nil
}

// guests is the guest scripts git would publish, by the name after the prefix, as repository paths.
type guests struct {
	acceptance map[string]string
	prepare    map[string]string
}

// guestScripts lists the published shell scripts directly under the guest directory.
func (g *gate) guestScripts() (guests, error) {
	listed, err := g.discoverShell()
	if err != nil {
		return guests{}, err
	}
	out := guests{acceptance: map[string]string{}, prepare: map[string]string{}}
	for _, rel := range listed {
		dir, base := path.Split(rel)
		name, ok := strings.CutSuffix(base, scriptSuffix)
		if dir != guestDir || !ok {
			continue
		}
		if id, isAcceptance := strings.CutPrefix(name, acceptancePrefix); isAcceptance && id != "" {
			out.acceptance[id] = rel
		}
		if id, isPrepare := strings.CutPrefix(name, preparePrefix); isPrepare && id != "" {
			out.prepare[id] = rel
		}
	}
	return out, nil
}

// discoverShell asks the shared discovery for the published shell scripts, as repository paths.
func (g *gate) discoverShell() ([]string, error) {
	var out, errs bytes.Buffer
	env := discovery.Env{Stdout: &out, Stderr: &errs, Cwd: g.root, Runner: g.env.Runner}
	args := []string{flagRoot, g.root, flagScripts, g.scripts, "shell"}
	if code := discovery.Main(args, env); code != discovery.ExitOK {
		return nil, errors.New(strings.TrimPrefix(strings.TrimSpace(errs.String()), "ERROR: "))
	}
	var files []string
	for abs := range strings.SplitSeq(out.String(), "\x00") {
		if abs == "" {
			continue
		}
		rel, err := filepath.Rel(g.root, abs)
		if err != nil {
			return nil, fmt.Errorf("discovery named %s outside the repository root", abs)
		}
		files = append(files, filepath.ToSlash(rel))
	}
	return files, nil
}

// checkPlanned compares every runnable scenario's planned count with the PLANNED of the checker
// that judges it. It returns how many runnable scenarios there are and the findings.
func (g *gate) checkPlanned(m shape, found guests) (int, []string, error) {
	var findings []string
	runnable := 0
	for _, sc := range m.scenarios {
		if sc.planned == nil {
			continue
		}
		runnable++
		rel, ok := found.acceptance[sc.id]
		if !ok {
			rel, ok = found.acceptance[sc.profile]
		}
		if !ok {
			findings = append(findings, fmt.Sprintf("%s: no guest checker at %s%s%s%s", sc.id, guestDir, acceptancePrefix, sc.profile, scriptSuffix))
			continue
		}
		finding, err := g.compare(sc, rel)
		if err != nil {
			return 0, nil, err
		}
		if finding != "" {
			findings = append(findings, finding)
		}
	}
	if runnable == 0 {
		return 0, nil, errors.New("no scenario declares planned, so the planned-count check examined nothing; a pass would vouch for nothing")
	}
	return runnable, findings, nil
}

// compare is the finding for one scenario against its checker, or "" when they agree.
func (g *gate) compare(sc scenario, rel string) (string, error) {
	data, err := g.readFile(rel)
	if err != nil {
		return "", err
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(data), rel)
	if err != nil {
		return "", fmt.Errorf("cannot parse %s: %w", rel, err)
	}
	name := path.Base(rel)
	assigns := plannedAssigns(file)
	switch {
	case len(assigns) == 0:
		return fmt.Sprintf("%s: no PLANNED=<n> line in %s", sc.id, name), nil
	case len(assigns) > 1:
		return fmt.Sprintf("%s: %s assigns PLANNED more than once", sc.id, name), nil
	}
	declared, ok := plainInteger(assigns[0])
	switch {
	case !ok:
		return fmt.Sprintf("%s: PLANNED in %s is not a plain integer", sc.id, name), nil
	case declared != *sc.planned:
		return fmt.Sprintf("%s: manifest planned=%d but %s declares PLANNED=%d", sc.id, *sc.planned, name, declared), nil
	}
	return "", nil
}

// plannedAssigns is every assignment to PLANNED among the script's top-level statements: a plain
// one, or one in a readonly/declare/typeset. A prefix assignment to a command, and anything inside
// a function or another compound command, is not the script's declaration.
func plannedAssigns(file *syntax.File) []*syntax.Assign {
	var found []*syntax.Assign
	for _, stmt := range file.Stmts {
		switch cmd := stmt.Cmd.(type) {
		case *syntax.CallExpr:
			if len(cmd.Args) == 0 {
				found = append(found, namedPlanned(cmd.Assigns)...)
			}
		case *syntax.DeclClause:
			found = append(found, namedPlanned(cmd.Args)...)
		}
	}
	return found
}

func namedPlanned(assigns []*syntax.Assign) []*syntax.Assign {
	var found []*syntax.Assign
	for _, a := range assigns {
		if a.Name != nil && a.Name.Value == "PLANNED" && !a.Naked {
			found = append(found, a)
		}
	}
	return found
}

// plainInteger is the value of an assignment that is a single unquoted decimal literal.
func plainInteger(a *syntax.Assign) (int, bool) {
	if a.Append || a.Array != nil || a.Index != nil || a.Value == nil || len(a.Value.Parts) != 1 {
		return 0, false
	}
	lit, ok := a.Value.Parts[0].(*syntax.Lit)
	if !ok || lit.Value == "" || strings.Trim(lit.Value, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(lit.Value)
	return n, err == nil
}

// checkFixtures judges every published guest-prepare script: it must belong to a scenario that
// reboots before its checks, because vmtest runs a fixture only on that branch. It returns how
// many fixtures are in order and the findings.
func checkFixtures(m shape, found guests) (int, []string) {
	byID := map[string]scenario{}
	for _, sc := range m.scenarios {
		byID[sc.id] = sc
	}
	var findings []string
	fine := 0
	for _, id := range slices.Sorted(maps.Keys(found.prepare)) {
		name := path.Base(found.prepare[id])
		sc, ok := byID[id]
		switch {
		case !ok:
			findings = append(findings, name+" belongs to no scenario in the manifest, so it would be deployed and never run")
		case sc.reboot != true:
			findings = append(findings, fmt.Sprintf("%s: %s exists but reboot_before_checks is %s, so the fixture would never run", id, name, describe(sc)))
		default:
			fine++
		}
	}
	return fine, findings
}

// describe says what a scenario declared for reboot_before_checks.
func describe(sc scenario) string {
	if !sc.hasReboot {
		return "not declared"
	}
	return fmt.Sprintf("%#v", sc.reboot)
}

// checkOrphans finds the guest-acceptance scripts that no scenario and no base profile would ever
// select: they are deployed and never run.
func checkOrphans(m shape, found guests) []string {
	ids := map[string]bool{}
	for _, sc := range m.scenarios {
		ids[sc.id] = true
	}
	var findings []string
	for _, name := range slices.Sorted(maps.Keys(found.acceptance)) {
		if !ids[name] && !m.profiles[name] {
			findings = append(findings, path.Base(found.acceptance[name])+" is the checker of no scenario and of no base profile, so it would be deployed and never run")
		}
	}
	return findings
}
