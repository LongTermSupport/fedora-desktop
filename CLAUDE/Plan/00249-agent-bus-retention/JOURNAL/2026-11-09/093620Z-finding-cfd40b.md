## 09:36 · finding · —

First host run of triage.bash, at F44 6ca12f7f6 through meta-deploy: exit 1, one FAILED leg (v1 per-team storage), but the measurement is complete enough to use.

- Four v1 teams (anonymised here as A to D). The whole of the agent-bus state is about 608 MB.
- WAL archive (db/archive) is the largest item for three of the four teams: 128, 130 and 53 MB, each never pruned since its team began (mid-October), growing about 4-14 MB a day on the busy teams. Team A's archive holds only two days (18 MB), its live db is the largest (43 MB).
- Live db 6-43 MB per team; backups 10-117 MB (7 kept each); media 0; state tars under 0.1 MB.
- Filesystem: 452 GB free of 1696 GB. Not an emergency, but the archive grows without bound.

The failed leg is a script defect, not a finding: its du probes glob a root-only team directory in the unprivileged shell, so du is handed the literal pattern. The meta and find probes, run under sudo, worked. Filed as an issue; fix then re-queue.
