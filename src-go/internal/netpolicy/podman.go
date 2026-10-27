package netpolicy

import "encoding/json"

// container is what the policy reads of a container from `inspect`.
type container struct {
	State struct {
		Pid     int  `json:"Pid"`
		Running bool `json:"Running"`
	} `json:"State"`
	ResolvConfPath string `json:"ResolvConfPath"`
	HostConfig     struct {
		Privileged bool `json:"Privileged"`
	} `json:"HostConfig"`
	EffectiveCaps   []string `json:"EffectiveCaps"`
	NetworkSettings struct {
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
}
