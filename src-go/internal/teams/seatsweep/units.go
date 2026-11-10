package seatsweep

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"

// The owner's two user units (C 3.7.5, DC21).
const (
	ServiceName = "teamchat-seats.service"
	TimerName   = "teamchat-seats.timer"
)

// userUnitDir is where the play puts them, under the owner's home.
const userUnitDir = ".config/systemd/user/"

// Unit is one user unit: its name, its path under the owner's home and its text.
type Unit struct {
	Name, Path, Text string
}

// poolCondition is the owner's pool with %S, the user manager's state directory, as its state
// home (paths.SeatStore's layout): a host where no seat was ever placed starts no process (L6).
const poolCondition = "%S/" + paths.Product + "/seats/pool"

// service is the sweep, as the owner. ccy-teams is where play-claude-yolo.yml installs it.
const service = `[Unit]
Description=Sweep the owner's ` + paths.Product + ` pool seats: release, leave, announce
ConditionPathIsDirectory=` + poolCondition + `

[Service]
Type=oneshot
ExecStart=%h/.local/bin/ccy-teams seats sweep
TimeoutStartSec=120
NoNewPrivileges=yes
`

// timer runs the sweep once a minute while the owner's user manager runs.
const timer = `[Unit]
Description=Sweep the owner's ` + paths.Product + ` pool seats once a minute

[Timer]
OnActiveSec=30s
OnUnitActiveSec=60s
AccuracySec=5s

[Install]
WantedBy=timers.target
`

// Units is both units, in the order `seats units list` gives them.
func Units() []Unit {
	return []Unit{
		{ServiceName, userUnitDir + ServiceName, service},
		{TimerName, userUnitDir + TimerName, timer},
	}
}
