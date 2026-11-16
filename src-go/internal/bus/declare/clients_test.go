package declare

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clientCases is testdata/client-cases.json, the resolution cases the plan's acceptance Python
// is tested on too.
type clientCases struct {
	Resolves []struct {
		Name     string            `json:"name"`
		HostVars string            `json:"hostVars"`
		Clients  map[string]string `json:"clients"`
		Default  string            `json:"default"`
	} `json:"resolves"`
	Refuses []struct {
		Name     string `json:"name"`
		HostVars string `json:"hostVars"`
		GoSays   string `json:"goSays"`
	} `json:"refuses"`
}

func readClientCases(t *testing.T) clientCases {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "client-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases clientCases
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Resolves) == 0 || len(cases.Refuses) == 0 {
		t.Fatal("testdata/client-cases.json holds no cases")
	}
	return cases
}

// The cases DESIGN-client-choice.md names (no setting, the home-level setting only, a team's
// own override) and more, from the table the plan's acceptance Python shares.
func TestMatrixClientsResolvesTheSharedCases(t *testing.T) {
	for _, c := range readClientCases(t).Resolves {
		got, err := HostVars(c.HostVars).MatrixClients()
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got.Default != c.Default || got.Of("nobody") != c.Default {
			t.Errorf("%s: default %q, a team not listed %q, want %q", c.Name, got.Default, got.Of("nobody"), c.Default)
		}
		for team, want := range c.Clients {
			if got.Of(team) != want {
				t.Errorf("%s: %s opens in %q, want %q", c.Name, team, got.Of(team), want)
			}
		}
	}
}

func TestMatrixClientsRefusesTheSharedRefusals(t *testing.T) {
	for _, c := range readClientCases(t).Refuses {
		_, err := HostVars(c.HostVars).MatrixClients()
		if !isRefusal(err) || !strings.Contains(err.Error(), c.GoSays) {
			t.Errorf("%s: %v, want a refusal saying %q", c.Name, err, c.GoSays)
		}
	}
}

func TestMatrixClientsRefusesATaggedValue(t *testing.T) {
	_, err := HostVars("agent_bus_matrix_client: !vault |\n  $ANSIBLE_VAULT;1.1;AES256\n").MatrixClients()
	if !isVaulted(err) {
		t.Fatalf("a tagged setting is not read: %v", err)
	}
	var e *Error
	if !errors.As(err, &e) || !strings.Contains(e.Message, "agent_bus_matrix_client") {
		t.Fatalf("the refusal names the setting: %v", err)
	}
}

// A commented or nested key of the same name is not the setting.
func TestMatrixClientsReadsOnlyTheTopLevelKey(t *testing.T) {
	got, err := HostVars("# agent_bus_matrix_client: element\nother:\n  agent_bus_matrix_client: element\n").MatrixClients()
	if err != nil || got.Default != ClientLightning {
		t.Fatalf("%+v %v", got, err)
	}
}
