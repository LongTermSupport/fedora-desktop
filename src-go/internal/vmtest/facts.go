package vmtest

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// baseFacts is one base of the manifest, as the validator reports it (E9).
type baseFacts struct {
	Key, Name, Kind, Profile, Tree string
	VCPUs, RAMMiB                  int
}

// envPair is one RUN_BASH_* override a scenario sets (E11).
type envPair struct{ Key, Value string }

// scenarioFacts is one scenario of the manifest, as the validator reports it (E10).
type scenarioFacts struct {
	ID, BaseKey, BaseName, Profile, Planned, MaxSkipped string
	Runnable, HostOnly, Reboot                          bool
	RunEnv                                              []envPair
}

var runEnvRE = regexp.MustCompile(`^(RUN_BASH_[A-Z_]+)=([A-Za-z0-9._,:/=-]+)$`)

// markerFields reads the one line `<marker> k=v ...` the validator prints: every key in want
// exactly once and nothing else.
func markerFields(out, marker string, want []string) (map[string]string, error) {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], marker+" ") {
		return nil, failf("unexpected validator output: %s", strings.TrimSpace(out))
	}
	known := map[string]bool{}
	for _, k := range want {
		known[k] = true
	}
	got := map[string]string{}
	for field := range strings.FieldsSeq(strings.TrimPrefix(lines[0], marker+" ")) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || !known[key] {
			return nil, failf("unknown field in validator marker: %s", field)
		}
		if _, dup := got[key]; dup {
			return nil, failf("field %s twice in validator marker: %s", key, lines[0])
		}
		got[key] = value
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			return nil, failf("validator marker has no %s: %s", k, lines[0])
		}
	}
	return got, nil
}

func positive(field, value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 || strconv.Itoa(n) != value {
		return 0, failf("validator reported %s='%s', which is not a positive whole number", field, value)
	}
	return n, nil
}

func parseBaseMarker(out string) (baseFacts, error) {
	f, err := markerFields(out, "VMTEST-BASE", []string{"key", "name", "kind", fieldProfile, "tree", "vcpus", "ram_mib"})
	if err != nil {
		return baseFacts{}, err
	}
	b := baseFacts{Key: f["key"], Name: f["name"], Kind: f["kind"], Profile: f[fieldProfile], Tree: f["tree"]}
	if slices.Contains([]string{b.Key, b.Name, b.Kind, b.Profile, b.Tree}, "") {
		return baseFacts{}, failf("validator marker has an empty field: %s", strings.TrimSpace(out))
	}
	if b.VCPUs, err = positive("vcpus", f["vcpus"]); err != nil {
		return baseFacts{}, err
	}
	if b.RAMMiB, err = positive("ram_mib", f["ram_mib"]); err != nil {
		return baseFacts{}, err
	}
	return b, nil
}

// flag is a marker boolean: exactly true or false. Anything else is the validator and this CLI
// disagreeing, and defaulting either way would decide what a run is (E10).
func flag(field, value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, failf("validator reported %s='%s', which is neither true nor false", field, value)
}

func parseScenarioMarker(out string) (scenarioFacts, error) {
	f, err := markerFields(out, "VMTEST-SCENARIO", []string{
		"id", "base", "base_name", fieldProfile, "planned",
		"max_skipped", "runnable", "host_only", "reboot_before_checks", "run_env",
	})
	if err != nil {
		return scenarioFacts{}, err
	}
	s := scenarioFacts{
		ID: f["id"], BaseKey: f["base"], BaseName: f["base_name"], Profile: f[fieldProfile],
		Planned: f["planned"], MaxSkipped: f["max_skipped"],
	}
	if slices.Contains([]string{s.ID, s.BaseKey, s.BaseName, s.Profile, s.Planned, s.MaxSkipped, f["run_env"]}, "") {
		return scenarioFacts{}, failf("validator marker has an empty field: %s", strings.TrimSpace(out))
	}
	for _, b := range []struct {
		field string
		into  *bool
	}{{"runnable", &s.Runnable}, {"host_only", &s.HostOnly}, {"reboot_before_checks", &s.Reboot}} {
		if *b.into, err = flag(b.field, f[b.field]); err != nil {
			return scenarioFacts{}, err
		}
	}
	if f["run_env"] != "-" {
		for pair := range strings.SplitSeq(f["run_env"], ",") {
			m := runEnvRE.FindStringSubmatch(pair)
			if m == nil {
				return scenarioFacts{}, failf("malformed run_env entry: %s", pair)
			}
			s.RunEnv = append(s.RunEnv, envPair{m[1], m[2]})
		}
	}
	return s, nil
}

// validator runs validate_manifest for one base or scenario, with the manifest on stdin.
func (c *cli) validator(ctx context.Context, l *lab, flag, value string) (string, error) {
	m, err := l.manifest()
	if err != nil {
		return "", err
	}
	res, err := c.helper(ctx, helperCall{
		module: "validate_manifest", stdin: bytes.NewReader(m),
		args: []string{flagFedoraVersion, l.fedora, flag, value},
	})
	if err != nil {
		return "", ctxErr(ctx, failf("the manifest validator refused %s %s: %v", flag, value, err))
	}
	return string(res.Stdout), nil
}

func (c *cli) baseFacts(ctx context.Context, l *lab, key string) (baseFacts, error) {
	out, err := c.validator(ctx, l, "--base", key)
	if err != nil {
		return baseFacts{}, err
	}
	return parseBaseMarker(out)
}

func (c *cli) scenarioFacts(ctx context.Context, l *lab, id string) (scenarioFacts, error) {
	out, err := c.validator(ctx, l, "--scenario", id)
	if err != nil {
		return scenarioFacts{}, err
	}
	return parseScenarioMarker(out)
}
