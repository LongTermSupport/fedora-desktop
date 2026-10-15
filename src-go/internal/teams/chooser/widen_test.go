package chooser

import "testing"

func TestTheThreeScopeChoicesAreWorded(t *testing.T) {
	o := scopeOptions("example-org/proj", "beta")
	if o.add != "Add example-org/proj to beta's scope" ||
		o.join != "Join without adding (beta will treat example-org/proj references as external)" ||
		o.leave != "Leave beta out" {
		t.Fatalf("%+v", o)
	}
}
