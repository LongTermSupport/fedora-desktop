## 20:37 · action · — — Task A.4: the fixed launcher table

Task A.4 built. The class is "host executes a session record's launcher path". The Go rule `project_record_launcher_exec` was red on exactly two reads: `sessions/restore.go:565` (the restore's start) and `teams/restart/jobs.go:302` (a team restart's start of a stopped session, which the sweep found). Both now start `registry.Launcher`'s program, the fixed table by prefix, and an unknown prefix is refused as `launcher-unknown`.

Suites reach their fakes through `CCY_SESSIONS_LAUNCHER_DIR`. It is host-only environment, the same trust as `CCY_LIB`.

The CCY version gate counts `internal/registry` as CCY code, so the change is ccy 3.124.111.
