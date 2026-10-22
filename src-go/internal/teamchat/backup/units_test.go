package backup

import (
	"slices"
	"strings"
	"testing"
)

func lines(text string) []string { return strings.Split(strings.TrimSuffix(text, "\n"), "\n") }

// TestTimerUnitIsC3121 is BU1's timer: daily, spread over an hour, caught up after a down time.
func TestTimerUnitIsC3121(t *testing.T) {
	got := lines(TimerUnit)
	for _, want := range []string{"[Timer]", "OnCalendar=daily", "RandomizedDelaySec=1h", "Persistent=true", "[Install]", "WantedBy=timers.target"} {
		if !slices.Contains(got, want) {
			t.Fatalf("the timer lacks %q:\n%s", want, TimerUnit)
		}
	}
}

// TestServiceUnitIsC3121 is BU1's service: `backup --all`, the health unit's sandbox with the
// backup's write paths and no CAP_SYS_PTRACE.
func TestServiceUnitIsC3121(t *testing.T) {
	got := lines(ServiceUnit)
	writePaths := "ReadWritePaths=/var/lib/teamchat-admin /var/lib/teamchat-status /run/teamchat-admin"
	want := []string{
		"[Service]",
		"Type=oneshot",
		"ExecStart=/usr/local/sbin/teamchat-admin backup --all",
		"TimeoutStartSec=30min",
		"SyslogIdentifier=teamchat-backup",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		writePaths,
		"ProtectHome=yes",
		"PrivateTmp=yes",
		"PrivateDevices=yes",
		"ProtectKernelTunables=yes",
		"ProtectKernelModules=yes",
		"ProtectKernelLogs=yes",
		"ProtectControlGroups=yes",
		"CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID",
		"SystemCallFilter=~ptrace process_vm_readv process_vm_writev",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
		"IPAddressDeny=any",
		"IPAddressAllow=localhost",
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("the service lacks %q:\n%s", w, ServiceUnit)
		}
	}
	if strings.Contains(ServiceUnit, "CAP_SYS_PTRACE") || strings.Contains(ServiceUnit, "Restart=") {
		t.Fatalf("the service holds what it must not:\n%s", ServiceUnit)
	}
	for _, l := range got {
		if strings.HasPrefix(l, "ReadWritePaths=") && l != writePaths {
			t.Fatalf("a second write path line %q", l)
		}
	}
}
