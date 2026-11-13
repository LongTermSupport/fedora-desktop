package badunit

import "strings"

// installed is kept running and wanted at boot.
const installed = "[Unit]\n" +
	"Description=a daemon\n" +
	"\n[Service]\n" +
	restartLines +
	"\n[Install]\n" +
	"WantedBy=multi-user.target\n"

// installedLines is a list of lines with its [Install].
func installedLines(user string) []string {
	return []string{
		"[Unit]",
		"Description=session for " + user,
		"[Service]",
		"Restart=always",
		"[Install]",
		"WantedBy=multi-user.target",
	}
}

// oneshot is started by a timer and never kept running: static on purpose.
const oneshot = `[Unit]
Description=a tick

[Service]
Type=oneshot
ExecStart=/usr/bin/tick
`

// dropIn is a drop-in: it has no Description=, and its base unit carries the [Install].
func dropIn(exec string) string {
	return "# managed\n[Service]\n" + "ExecStart=" + exec + "\nRestart=always\n"
}

// noService is not a unit text at all.
const noService = "Restart=always is how the units restart"

// head without its own [Service] header: the unit text is judged where [Service] is written.
func viaHead(sandbox []string) string {
	return installed + strings.Join(sandbox, "\n")
}

// restartOnFailure is a oneshot retried on failure: not a unit kept running.
const restartOnFailure = "[Unit]\nDescription=a retried tick\n[Service]\nType=oneshot\nRestart=on-failure\n"

var _ = []any{installedLines, oneshot, dropIn, noService, viaHead, restartOnFailure}
