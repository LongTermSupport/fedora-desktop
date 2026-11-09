package netpolicy

import (
	"encoding/json"
	"testing"
)

// A `podman inspect` document carries far more than the policy reads; the fields it reads
// are podman's own names.
func TestContainerDecodesPodmanInspect(t *testing.T) {
	doc := `[{"Id":"0123","State":{"Status":"created","Running":false,"Pid":4242},"ResolvConfPath":"/run/r/resolv.conf",
	"HostConfig":{"Privileged":false,"CapAdd":[]},"EffectiveCaps":["CAP_CHOWN","CAP_KILL"],
	"NetworkSettings":{"Networks":{"podman":{"IPAddress":"x"},"shop-network":{}}}}]`
	var cs []container
	if err := json.Unmarshal([]byte(doc), &cs); err != nil {
		t.Fatal(err)
	}
	c := cs[0]
	if c.State.Pid != 4242 || c.State.Running || c.ResolvConfPath != "/run/r/resolv.conf" || c.HostConfig.Privileged ||
		len(c.EffectiveCaps) != 2 || len(c.NetworkSettings.Networks) != 2 {
		t.Errorf("%+v", c)
	}
}
