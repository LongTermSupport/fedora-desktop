package nvidiastatus

import "testing"

// E25: the verdict's order, the first that holds.
func TestVerdictOrder(t *testing.T) {
	installed := []pkg{{name: "akmod-nvidia", state: yes}}
	working := outcome{name: "nvidia-smi"}
	broken := outcome{name: "nvidia-smi", code: 9}
	for _, c := range []struct {
		name string
		f    facts
		want verdict
	}{
		{"loaded and working", facts{nvidiaLoaded: true, smiPresent: true, smi: working, pkgs: installed, built: unknown}, complete},
		{"mok before not installed", facts{sb: sbEnabled, mok: mokNotEnrolled, pkgs: []pkg{{name: "akmod-nvidia", state: no}}}, mokRequired},
		{"mok only under secure boot", facts{sb: sbUnknown, mok: mokNotEnrolled, pkgs: installed, built: yes}, rebootRequired},
		{"not installed", facts{pkgs: []pkg{{name: "akmod-nvidia", state: no}}, built: no}, driverMissing},
		{"building", facts{pkgs: installed, built: no}, building},
		{"reboot", facts{pkgs: installed, built: yes}, rebootRequired},
		{"modules unread", facts{pkgs: installed, built: yes, modulesErr: errStart}, incomplete},
		{"built unknown", facts{pkgs: installed, built: unknown}, incomplete},
		{"loaded, smi fails", facts{nvidiaLoaded: true, smiPresent: true, smi: broken, pkgs: installed, built: yes}, incomplete},
	} {
		if got := judge(&c.f); got != c.want {
			t.Errorf("%s: verdict %d, want %d", c.name, got, c.want)
		}
	}
}
