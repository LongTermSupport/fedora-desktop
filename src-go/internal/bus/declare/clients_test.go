package declare

import (
	"errors"
	"strings"
	"testing"
)

// The three cases DESIGN-client-choice.md names, which the play's Jinja
// (play-agent-bus-element.yml agent_bus_element_profiles) resolves the same way: no setting, the
// home-level setting only, and a team's own override.
func TestMatrixClientsResolvesAsThePlayDoes(t *testing.T) {
	const teams = "agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n" +
		"  - team: beta\n    base_url: http://192.0.2.1:6168\n    client: lightning\n"
	cases := []struct {
		name, text                   string
		alpha, beta, undeclared, def string
	}{
		{
			"no setting", "agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n",
			ClientElement, ClientElement, ClientElement, ClientElement,
		},
		{
			"the home-level setting only", "agent_bus_matrix_client: lightning\n" +
				"agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n",
			ClientLightning, ClientLightning, ClientLightning, ClientLightning,
		},
		{"a team override", teams, ClientElement, ClientLightning, ClientElement, ClientElement},
		{
			"an override back to element", "agent_bus_matrix_client: 'lightning'  # trying it\n" +
				"agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n    client: element\n",
			ClientElement, ClientLightning, ClientLightning, ClientLightning,
		},
		{
			"no list at all", "agent_bus_matrix_client: lightning\n",
			ClientLightning, ClientLightning, ClientLightning, ClientLightning,
		},
	}
	for _, c := range cases {
		got, err := HostVars(c.text).MatrixClients()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got.Default != c.def || got.Of("alpha") != c.alpha || got.Of("beta") != c.beta || got.Of("nobody") != c.undeclared {
			t.Errorf("%s: %+v gives alpha %q beta %q nobody %q", c.name, got, got.Of("alpha"), got.Of("beta"), got.Of("nobody"))
		}
	}
}

func TestMatrixClientsRefusesAnyOtherValue(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"a home-level value", "agent_bus_matrix_client: nheko\n", "agent_bus_matrix_client is 'nheko'; the client must be element or lightning"},
		{"an empty home-level value", "agent_bus_matrix_client:\n", "agent_bus_matrix_client is ''; the client must be element or lightning"},
		{
			"a team's value", "agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n    client: Lightning\n",
			"team alpha's client is 'Lightning'; the client must be element or lightning",
		},
		{
			"a team's value that is not text", "agent_bus_element_teams:\n  - team: alpha\n    base_url: http://192.0.2.1:6167\n    client: [\"lightning\"]\n",
			"team alpha's client is ['lightning']; the client must be element or lightning",
		},
		{"set twice", "agent_bus_matrix_client: element\nagent_bus_matrix_client: lightning\n", "agent_bus_matrix_client is set more than once"},
	}
	for _, c := range cases {
		_, err := HostVars(c.text).MatrixClients()
		if !isRefusal(err) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want a refusal saying %q", c.name, err, c.want)
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
	got, err := HostVars("# agent_bus_matrix_client: lightning\nother:\n  agent_bus_matrix_client: lightning\n").MatrixClients()
	if err != nil || got.Default != ClientElement {
		t.Fatalf("%+v %v", got, err)
	}
}
