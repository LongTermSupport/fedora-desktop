// Package badunit renders systemd units that are kept running and that nothing starts at boot.
package badunit

import "strings"

// restartLines are how the team units restart: always.
const restartLines = "Restart=always\n" +
	"RestartSec=5s\n"

// serverHead is teamchat-server@.service's head as it was (issue 247): a constant built from
// constants, with no [Install], so the unit is static and no reboot starts an instance.
const serverHead = "[Unit]\n" + // want `kept running by Restart=always with no \[Install\] section`
	"Description=v2 team homeserver %i\n" +
	"\n[Service]\n" +
	"Type=notify\n" +
	restartLines

// serverUnit adds the sandbox to the head; the head is the unit text the rule judges, once.
func serverUnit(sandbox []string) string {
	return serverHead + strings.Join(sandbox, "\n") + "\n"
}

// terminatorUnit is teamchat-tls@.service as it was: a concatenation with non-constant parts.
func terminatorUnit(sandbox []string) string {
	return "[Unit]\n" + // want `kept running by Restart=always with no \[Install\] section`
		"Description=v2 team TLS terminator %i\n" +
		"\n[Service]\n" +
		restartLines +
		strings.Join(sandbox, "\n") + "\n"
}

// rawUnit and rawService are two halves, neither a unit alone; joined, they are one.
const rawUnit = `[Unit]
Description=a daemon
`

const rawService = `
[Service]
ExecStart=/usr/bin/daemon
Restart=always
`

var joined = rawUnit + // want `kept running by Restart=always with no \[Install\] section`
	rawService

// plainRaw is one string literal with the whole unit.
const plainRaw = "[Unit]\nDescription=a daemon\n[Service]\nRestart=always\n" // want `kept running by Restart=always with no \[Install\] section`

// lines is a unit as a list of lines.
func lines(user string) []string {
	return []string{ // want `kept running by Restart=always with no \[Install\] section`
		"[Unit]",
		"Description=session for " + user,
		"",
		"[Service]",
		"User=" + user,
		"Restart=always",
	}
}

// installedElsewhere has an [Install] line that is not a section header: still static.
const installedElsewhere = "[Unit]\n" + // want `kept running by Restart=always with no \[Install\] section`
	"Description=a daemon\n" +
	"\n[Service]\n" +
	"Restart=always\n" +
	"# [Install] is added later\n"

var _ = []any{serverUnit, terminatorUnit, plainRaw, lines, installedElsewhere, joined}
