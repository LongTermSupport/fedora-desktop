package vmtest

import (
	"strings"
	"testing"
)

// E9: the base marker is read strictly.
func TestE9BaseMarker(t *testing.T) {
	good := "VMTEST-BASE key=server-fast name=server-fast-44 kind=fast profile=server tree=- vcpus=2 ram_mib=4096\n"
	b, err := parseBaseMarker(good)
	if err != nil {
		t.Fatal(err)
	}
	if b != (baseFacts{Key: "server-fast", Name: "server-fast-44", Kind: "fast", Profile: "server", Tree: "-", VCPUs: 2, RAMMiB: 4096}) {
		t.Fatalf("got %+v", b)
	}
	bad := map[string]string{
		"another marker":  "VMTEST-SCENARIO id=x\n",
		"two lines":       good + good,
		"an unknown key":  strings.TrimSuffix(good, "\n") + " colour=red\n",
		"a missing key":   strings.Replace(good, " tree=-", "", 1),
		"a repeated key":  strings.TrimSuffix(good, "\n") + " kind=full\n",
		"vcpus not whole": strings.Replace(good, "vcpus=2", "vcpus=2x", 1),
		"ram zero":        strings.Replace(good, "ram_mib=4096", "ram_mib=0", 1),
		"a bare word":     strings.Replace(good, "kind=fast", "fast", 1),
		"nothing":         "",
	}
	for name, text := range bad {
		if _, err := parseBaseMarker(text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

const goodScenario = "VMTEST-SCENARIO id=s base=server-fast base_name=server-fast-44 profile=server planned=5 max_skipped=0 runnable=true host_only=false reboot_before_checks=true run_env=RUN_BASH_PLAY=x.yml,RUN_BASH_PROFILE=odd\n"

// E10, E11: the scenario marker; the booleans are exactly true or false, run_env is checked.
func TestE10ScenarioMarker(t *testing.T) {
	good := goodScenario
	s, err := parseScenarioMarker(good)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Runnable || s.HostOnly || !s.Reboot || s.BaseKey != "server-fast" || s.Planned != "5" ||
		len(s.RunEnv) != 2 || s.RunEnv[1] != (envPair{"RUN_BASH_PROFILE", "odd"}) {
		t.Fatalf("got %+v", s)
	}
	none, err := parseScenarioMarker(strings.Replace(good, "run_env=RUN_BASH_PLAY=x.yml,RUN_BASH_PROFILE=odd", "run_env=-", 1))
	if err != nil || len(none.RunEnv) != 0 {
		t.Fatalf("got %+v %v", none, err)
	}
}

// E10, E11: a spoiled scenario marker is refused, and the refusal names the field.
func TestE10ScenarioMarkerRefusals(t *testing.T) {
	good := goodScenario
	for _, spoil := range [][2]string{
		{"host_only=false", "host_only=yes"},
		{"host_only=false", "host_only="},
		{"runnable=true", "runnable=True"},
		{"reboot_before_checks=true", "reboot_before_checks=1"},
		{"RUN_BASH_PROFILE=odd", "PATH=/tmp"},
		{"RUN_BASH_PROFILE=odd", "RUN_BASH_PROFILE=a'b"},
		{"RUN_BASH_PROFILE=odd", "RUN_BASH_PROFILE="},
	} {
		if _, err := parseScenarioMarker(strings.Replace(good, spoil[0], spoil[1], 1)); err == nil {
			t.Errorf("%s accepted", spoil[1])
		}
	}
	_, err := parseScenarioMarker(strings.Replace(good, "host_only=false", "host_only=maybe", 1))
	if err == nil || !strings.Contains(err.Error(), "validator reported host_only='maybe', which is neither true nor false") {
		t.Fatalf("got %v", err)
	}
	_, err = parseScenarioMarker(strings.Replace(good, "RUN_BASH_PROFILE=odd", "PATH=/tmp", 1))
	if err == nil || !strings.Contains(err.Error(), "malformed run_env entry: PATH=/tmp") {
		t.Fatalf("got %v", err)
	}
}
