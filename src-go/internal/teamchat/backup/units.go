package backup

import "github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"

// TimerUnit is `teamchat-backup.timer` (C 3.12.1, BU1). Nothing installs it here (gate A):
// Task 2.5's play renders it.
const TimerUnit = `[Unit]
Description=Daily backups of every ` + paths.Product + ` team

[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
`

// ServiceUnit is `teamchat-backup.service` (C 3.12.1, BU1): `backup --all` in the health unit's
// sandbox (3.11.1), writing the tars, the records and the locks, without CAP_SYS_PTRACE.
const ServiceUnit = `[Unit]
Description=Backups of every ` + paths.Product + ` team

[Service]
Type=oneshot
ExecStart=` + paths.AdminBinaryPath + ` backup --all
TimeoutStartSec=30min
SyslogIdentifier=` + paths.SyslogBackup + `
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=` + paths.AdminStateRoot + ` ` + paths.StatusRoot + ` ` + paths.AdminRunDir + `
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_CHOWN CAP_SETUID CAP_SETGID
SystemCallFilter=~ptrace process_vm_readv process_vm_writev
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
IPAddressDeny=any
IPAddressAllow=localhost
`
