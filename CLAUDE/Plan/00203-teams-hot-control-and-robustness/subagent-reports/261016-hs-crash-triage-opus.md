# Homeserver and watcher stops: what the repo evidence shows, and the host triage

Owner's question: why do the team homeservers crash, and do we need a different tech stack?
This report answers from the evidence this repo already holds, and queues
`triage-hs.bash` for the host to answer from the journal. The container cannot read the
host journal. Real teams appear as team-A, team-B and team-C. Throwaway acceptance teams
keep their names. Run-log folder stamps are local time (UTC+1).

## The answer so far: no crash is on record

No log, journal or report in this repo shows a homeserver crash. Nothing records a
`code=dumped`, a `Watchdog timeout`, an OOM kill, a SIGABRT or a Tuwunel panic. Every
recorded homeserver stop was caused by us. The one watcher death with a date was caused by
our own play restarting the homeserver, against watcher code that was later fixed.
Whether the journal holds stops these logs never saw is what `triage-hs.bash` will answer.

## Homeserver stops (`agent-bus-hs@<team>.service`)

| #   | When                                 | Team                                                  | What the evidence says                                                                                                                                                                                     | Source                                                                                 | Caused by                                                      |
| --- | ------------------------------------ | ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| H1  | 10-07, three times                   | acceptance                                            | "Received SIGTERM" … "Deactivated successfully" at teardown                                                                                                                                                | `untracked/plan-runs/00161-…/acceptance/20261008-160947/acceptance.log` (journal dump) | us: acceptance teardown                                        |
| H2  | 10-08 ~10:22 local                   | zz-deploy-check                                       | "is activating, not active"; no exit code or signal in the log                                                                                                                                             | `00161-…/deploy/20261008-102217/deploy.log:33`; 00161 journal 26-10-08                 | unknown from the log; the 00161 journal ties it to the H3 race |
| H3  | 10-08 16:09:53 local                 | acceptance                                            | "Sent signal SIGUSR2 … on client request", "code=killed, status=12/USR2", "Result 'signal'". `backup-now` signalled Tuwunel before Tuwunel had installed its handler (since fixed: it waits for readiness) | `00161-…/acceptance/20261008-160947/acceptance.log:61-64`                              | us: our backup's signal                                        |
| H4  | every acceptance run, 10-07 to 10-16 | zz-deploy-check, acceptance, accept201a/b, acc-robust | created, then "stopped and disabled" at teardown; "restarted agent-bus-hs@acceptance.service" on U23's allow_from change                                                                                   | e.g. `00161-…/acceptance/20261016-084846/team-u23.out`                                 | us: acceptance                                                 |
| H5  | 10-15, 21:19:36 to 21:20:03 local    | team-A, B, C                                          | `play-agent-bus.yml`: backup, then each team changed (the Tuwunel 1.9.4 swap)                                                                                                                              | `_meta-deploy/20261015-211704/play-agent-bus.yml.log`                                  | us: deploy                                                     |
| H6  | 10-16 ~02:18 local                   | team-A, B, C                                          | the new unit (notify, watchdog) deployed, each team changed                                                                                                                                                | `_meta-deploy/20261016-021854/play-agent-bus.yml.log`                                  | us: deploy                                                     |
| H7  | several play runs, 10-13 to 10-16    | team-A, B, C                                          | the play shows "changed" per team; the Ansible log cannot say whether a restart happened                                                                                                                   | `_meta-deploy/<run>/play-agent-bus.yml.log`                                            | us, if any restart happened                                    |

The real teams were never seen down. Every `ccy-teams --list` capture shows them up, and
`agent-bus health status` exited 0 with no PROBLEM line (00203 journal, 10-16). Acceptance
R1 passed: no restart in 75 s while healthy. R3 to R5, which kill the homeserver on
purpose, have not run yet.

## Health check

| #   | When                     | What                                                                                                                                | Caused by              |
| --- | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| HT1 | 10-16 ~02:18-02:52 local | `agent-bus-hs-health@` failed every minute with exit 78: it asked a .service for timer properties. The homeservers were unaffected. | us: a bug in the check |
| HT2 | 10-16 ~02:52 local       | the play's `reset-failed` cleared those runs                                                                                        | us: play               |

## Watcher (`pingbus watch`, inside each ccy container)

| #   | When                       | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | Caused by                                                       |
| --- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| W1  | 10-15 21:20:01 local       | **Now dated.** This session's `/tmp/pingbus-home/watch.log` ends with: `team <team-A>: homeserver: GET /_matrix/client/v3/sync: URLError: this team is no longer watched`, then `no team is left to watch: the watcher exits`. No supervisor line follows. The time falls inside H5's play run (21:19:36 to 21:20:03). The watcher was code from before 28ec8b1d (10-16 01:01), which treated a refused connection as permanent. The current `matrix.py` raises `Unreachable(transient=True)` for it, and a supervisor now restarts the child. | us: H5's restart, met by watcher code that has since been fixed |
| W2  | 25 runs, 10-09 to 10-16    | U20 M2.3 stops accc's watcher on purpose                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | us: acceptance                                                  |
| W3  | 10-16 ~04:45, ~05:38 local | M2.3 failures: the first found the watcher alive; the second was a respawn not seen within 240 s (the 300 s interval). Both are test artefacts, not real stops.                                                                                                                                                                                                                                                                                                                                                                                | us: acceptance                                                  |

Watchers also end at every U20 teardown, when their containers go.

## What `triage-hs.bash` will answer (queued in meta-deploy.list)

It is read-only and needs `sudo -n` only to read. The orchestrator is `triage-hs.bash`, the
probes are in `_triage-hs.inc.bash`, and the analysis is `hs_journal.py`, tested by
`test_hs_journal.py`. Reports go under `untracked/plan-runs/00203-…/triage-hs/<stamp>/`:

- `triage-hs-stops-report.txt`: every stop of every agent-bus-hs@ unit over the whole
  journal. Each comes with what systemd recorded: exit code or signal, Result=, restart
  counter, watchdog, OOM killer (kernel or systemd-oomd), core dump, and the memory peak at
  the stop. It also says who asked: health-restart, reboot/shutdown, acceptance or deploy/play
  (by the plan run or meta-deploy entry whose time window holds the stop, or an
  ansible/agent-bus-install line), a sudo command, or nobody. Each stop carries the unit's
  own log lines just before it and any kernel OOM line. Then the health check's restarts with
  their probe failure, its failed runs, and the backup runs and failures. Each stop also
  notes a backup started within 15 minutes before it, because SIGUSR2 has killed a homeserver
  once already (H3).
- `triage-hs-facts-report.txt`: the Tuwunel version and pin; each unit's limits and peaks
  now (there is no MemoryMax), NRestarts and its drop-in; each team's tuwunel.toml with
  secret-named string values substituted and checked; the database size, its filesystem,
  mount options and copy-on-write attribute (Tuwunel #653: a start that hangs after an unclean
  kill, on btrfs), and its free space and inodes (#649: writes panic on a full disk);
  `coredumpctl` for tuwunel; host memory and PSI; the boots; the meta-deploy runs; and each
  running container's watch.log, with counts of exits, supervisor restarts and dropped teams.
- stdout: per team, the stops split into deliberate, not deliberate and unknown, each with
  its categories, plus the largest memory peak and the database's filesystem and free space.
