package unitsandbox

import (
	"strings"
	"testing"
)

func TestTheThreeTogetherAreAPIDNamespaceFinding(t *testing.T) {
	text := "[Service]\nDynamicUser=yes\nStateDirectory=teamchat/%i\nPrivatePIDs=yes\n"
	got := CheckPIDNamespace(text)
	if len(got) != 1 || got[0].Line != 4 {
		t.Fatalf("findings %v", got)
	}
	if s := got[0].String(); !strings.HasPrefix(s, PIDNamespaceID+": line 4 ") || !strings.Contains(s, "41014") {
		t.Fatalf("message %q", s)
	}
}

func TestAnyTwoOfTheThreeAreNotAFinding(t *testing.T) {
	for _, text := range []string{
		"DynamicUser=yes\nStateDirectory=a\n",
		"DynamicUser=yes\nPrivatePIDs=yes\n",
		"StateDirectory=a\nPrivatePIDs=yes\n",
		"DynamicUser=no\nStateDirectory=a\nPrivatePIDs=yes\n",
		"DynamicUser=yes\nStateDirectory=a\nPrivatePIDs=no\n",
		"DynamicUser=yes\nStateDirectory=a\nStateDirectory=\nPrivatePIDs=yes\n",
	} {
		if got := CheckPIDNamespace(text); len(got) != 0 {
			t.Errorf("%q: findings %v", text, got)
		}
	}
}

// systemd's booleans and the property forms the tmpfs rule reads are read the same way here.
func TestThePIDNamespaceRuleReadsBooleansAndProperties(t *testing.T) {
	for _, text := range []string{
		"DynamicUser=true\nStateDirectory=a b\nPrivatePIDs=1\n",
		"\t\"DynamicUser=on\",\n\t\"StateDirectory=a\",\n\t\"PrivatePIDs=y\",\n",
		"--property=DynamicUser=yes \\\n--property=StateDirectory=a \\\n--property=PrivatePIDs=yes\n",
	} {
		if got := CheckPIDNamespace(text); len(got) != 1 {
			t.Errorf("%q: findings %v", text, got)
		}
	}
}

// The last assignment wins, as in systemd.
func TestTheLastAssignmentDecides(t *testing.T) {
	if got := CheckPIDNamespace("DynamicUser=yes\nStateDirectory=a\nPrivatePIDs=yes\nPrivatePIDs=no\n"); len(got) != 0 {
		t.Fatalf("findings %v", got)
	}
	if got := CheckPIDNamespace("DynamicUser=no\nDynamicUser=yes\nStateDirectory=a\nPrivatePIDs=yes\n"); len(got) != 1 {
		t.Fatalf("findings %v", got)
	}
}
