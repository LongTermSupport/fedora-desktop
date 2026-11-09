Plan 00246 Task 8.8, install-path stage A part 1 (Go only): builder report
==========================================================================

Branch feat/install-stage-a-units (from feat/install-stage-a). Commits: see the end.

WHY THIS FILE IS .md.txt: every .md write in this nested worktree
(.claude/worktrees/install-path-plan/.claude/worktrees/<agent>) is refused by the hooks
daemon's R-MARKDOWN-WRONG-LOCATION, the plan folder included. That is open issue #147, which
records that the earlier agents did not route around the guard with a shell write and that the
coordinator moved the text. I did the same: this report is a plain-text file at the brief's path
plus ".txt", and the two markdown edits the brief asks for (DESIGN-v2-C-host.md 3.2.3 and 3.3)
and the GoProjectRules.md entry are NOT made; their exact text is in section 7 for the
coordinator to apply (git mv this file to the .md name at the same time).


1. What was built, file by file
-------------------------------

src-go/internal/teamchat/paths/host.go, host_test.go
  HealthServiceFile, HealthTimerFile, BackupServiceFile, BackupTimerFile: the install paths
  (/etc/systemd/system/<name>) the units lacked. Added to hostNames (so the package's
  every-exported-name-is-sampled, v1-name and derivation checks cover them) and TestHostNames.

src-go/internal/teamchat/unitfiles/unitfiles.go (new package; `units` was taken by the D-Bus
package) and unitfiles_test.go, testdata/*.golden
  File{Name, Path, Text}; Files() in the fixed order (server, tls, health service, health timer,
  backup service, backup timer, resolv.conf); Names(); Lookup(name); ExecBinaries(text).
  - teamchat-server@.service: a const head (C 3.2.3's [Unit] and lifecycle lines) + host.BaseSandbox()
    with the LoadCredential/Environment pair inserted after StateDirectoryMode=, where C 3.2.3 has
    them. The %i paths are written as AdminStateRoot+"/%i/..." and a test holds them equal to
    paths.RestoreBlock/ServerConfig/HomeserverUser for a team, and that every BaseSandbox line
    is present in order.
  - teamchat-tls@.service: built from host.BaseSandbox() by filtering (see 3), so a new hardening
    line in the base sandbox reaches the terminator too.
  - teamchat-health.service, teamchat-health.timer: consts.
  - teamchat-backup.service/.timer: backup.ServiceUnit / backup.TimerUnit themselves (a test
    holds identity), not copies.
  - resolv.conf: new const (no Go held it): comment + "nameserver 127.0.0.1".
  - terminatorFileLimit = 2*tlsterm.DefaultMaxConns + 64 (576 today).
  - ExecBinaries(text): first word of each ExecStart= value, systemd prefixes -@:+! taken off,
    an empty ExecStart= (reset) ignored. Exported for the later play test (see 6).
  Goldens hold D 4.2's line as C 3.2.3 writes it, the placeholder
  `InaccessiblePaths=<D 4.2's six v1 paths, each with its "-">`, and the test substitutes
  v1names.InaccessiblePaths: the first qa-changed run failed teamchat-keeps-off-v1 on four
  goldens that spelt the v1 paths out (v1 names live in v1names alone); with the placeholder
  the server golden is the design's block byte for byte.
  Tests: order and paths; each text equals testdata/<name>.golden (go test -update rewrites;
  the server/tls/health/resolver goldens were written by hand from the design first, so the
  golden is evidence of faithfulness, not a copy of the output); backup identity; server holds
  the base sandbox; X18a clause by clause; the file limit; health writes StatusRoot,
  AdminRunDir and paths.TLSDir(team) and carries the filter/caps/six paths; timer values;
  resolver stub; Lookup; ExecBinaries over the files, over host.DropIns' two drop-ins
  (terminator -> /usr/local/bin/teamchat, homeserver -> the pinned tuwunel) and odd spellings.

src-go/internal/teamchat/v1names/units_test.go
  madeUnits now ends with installedServices(): every unitfiles file with a [Service] section,
  as installed (server, tls, health, backup), so TestEveryUnitTextCarriesTheSixV1Paths and
  TestEveryUnitTextDeniesWhatItDoesNotAllow judge the installed bytes. New
  TestEveryInstalledFileIsJudgedOrRunsNothing: every installed file is a service (judged), a
  timer or the resolver stub, and the services are exactly the four. unitSources gains
  unitfiles/unitfiles.go; the host/tlsunit.go row no longer says nothing makes the base unit.

src-go/internal/teamchat/v1names/staticunits_test.go (new; Defence Before Fix, see 4)
  TestNoTeamchatUnitIsAStaticFile, TestTheStaticUnitCheckDiscriminates,
  TestTheStaticUnitCheckReadsTheCheckout.

src-go/internal/teamchat/admin/unitsverb.go (new), unitsverb_test.go (new), run.go, verbs.go,
run_test.go
  `units list` and `units print <name>`, ClassAny, in buildTable (group(ClassAny, "units",
  "list", "print"), before version) and rootRuns. list: "<name>\t<path>\n" per file; extra
  arguments 64. print: exactly the text; no name, two names, or a name not in the list (an
  instance name, a path, a flag) is 64 with "the files are ..." naming all seven; the name is
  echoed strconv.Quote'd. Unwritable stdout is 1 (answer()). They need no System, so they
  run unwired. run_test.go: specVerbs gains them; TestEveryVerbEveryClass exempts them as it
  exempts version. Checked with the built binary: list, print resolv.conf, print nope (64).


2. Red, then green, per test class
----------------------------------

- paths: host_test.go edited first; `go test ./internal/teamchat/paths/` failed to build
  (undefined: HealthServiceFile, ...); green after host.go.
- unitfiles: test + hand-written goldens first; build failed (undefined: File, Lookup, Files,
  terminatorFileLimit, terminatorDrops); green after unitfiles.go, goldens matching on the
  first run.
- TestEveryUnitTextSourceIsJudged: with the import added and the unitfiles row withheld it
  failed: found [... "unitfiles/unitfiles.go"], judged [...without it]; green with the row.
- The six-paths and deny gates over the new texts: mutation of the health text (IPAddressDeny=any
  and the six-paths line removed) failed both: "the installed teamchat-health.service does not
  list [six paths] as inaccessible" and "has [IPAddressAllow=] with no deny of any"; reverted,
  green.
- Static-unit detector: a real-tree red: templates/teamchat-probe@.service.j2 created and
  `git add -N`'d, TestNoTeamchatUnitIsAStaticFile failed naming it; removed (git rm --cached,
  rm), green. The fixture tests are red cases kept in the suite: a list of 10 paths (5 found:
  a unit, a timer, a .j2, a drop-in .conf, a socket; not: a .golden, a .md, a v1 unit, a Go
  file, a teamchat-named script) and a git init'd temp checkout with one tracked and one
  untracked unit (only the tracked found).
- admin verb: tests first; TestVerbTableIsCA1, TestUsageNamesEveryVerb and the four units
  tests failed; green after unitsverb.go and the table rows.
- systemd itself: the printed units, with stub drop-ins, through `systemd-analyze verify`
  (systemd 257 in the container): no unknown directive or parse warning; the only findings are
  /usr/local/sbin/teamchat-admin missing here (health and backup).


3. Where each unit's bytes come from, and every departure
---------------------------------------------------------

teamchat-server@.service: DESIGN-v2-C-host.md lines 240-295, byte for byte, the placeholder line
295 expanded to v1names.InaccessiblePaths. No departure.

teamchat-tls@.service: X18a (SPEC-cross-network.md line 140), clause by clause:
  - no ExecStart=, no User=; DynamicUser=yes (kept from the base sandbox)
  - After= and Wants= teamchat-server@%i.service; no BindsTo=
  - Restart=always
  - the homeserver's hardening lines: every host.BaseSandbox line except User=,
    StateDirectory=, StateDirectoryMode= (no state: X18a "no state to share"),
    TemporaryFileSystem= and both BindReadOnlyPaths= (the resolver: X18a "no resolver"; tlsverb
    takes only IP literals for --listen and dials 127.0.0.1, it resolves no name), and the base
    RestrictAddressFamilies= line, replaced by "AF_INET AF_INET6" (X18a "no AF_UNIX": it reads
    its credentials from files and talks to no socket but TCP). Kept: UMask, NoNewPrivileges,
    empty CapabilityBoundingSet, ProtectSystem=strict ... SystemCallFilter, IPAccounting,
    InaccessiblePaths=-/run/dbus (it uses no D-Bus) and the six paths.
  - IPAddressDeny=any, SocketBindDeny=any (from the base sandbox)
  - LimitNOFILE=576 = 2*tlsterm.DefaultMaxConns + 64 (each connection two descriptors; the 64
    for listeners, stdio, the runtime's epoll and pipes), placed after SocketBindDeny=.
  Departures (my judgment, X18a is silent):
  - Description=v2 team TLS terminator %i.
  - StartLimitIntervalSec=0 and RestartSec=5s, RestartSteps=5, RestartMaxDelaySec=60s, the
    homeserver's restart lines: a terminator whose listen address is not up yet must keep
    retrying rather than hit the start limit.
  - Before=lxc.service, with a comment. Since X18 the homeserver binds loopback alone, so an
    LXC guest's member reaches it only through the terminator; the homeserver's own
    Before=lxc.service (C 3.2.3: goodbyes reach a live server at shutdown) is worth nothing if
    the terminator stops first. OPEN QUESTION 1.
  - No Type= (simple): tlsverb sends no sd_notify.

teamchat-health.service: C 3.11.1 (line 2206-2211), in backup.ServiceUnit's layout (which was
derived from it). Departures:
  - Description=Health of every teamchat team; SyslogIdentifier=teamchat-health
    (paths.SyslogHealth exists for it; the backup unit does the same).
  - ProtectKernel* expanded to Tunables, Modules, Logs.
  - ReadWritePaths= adds /var/lib/teamchat-admin (paths.AdminStateRoot) after the design's two,
    with a comment. Why: S12's tick (origin/feat/crossnet-s13-int, host/leaf.go LeafRenewer
    via health/leaf.go) writes leaf.key and leaf.crt with state.WriteFile into
    paths.TLSDir(team) = /var/lib/teamchat-admin/<team>/tls, and nothing else under the admin
    root (its other writes are the status records under StatusRoot and the locks under
    AdminRunDir). systemd's ReadWritePaths= takes no pattern, and the teams come and go, so
    a static unit can name the TLS directories only through their parent. This makes the
    whole admin root (config.toml, approvals, CA keys) writable to the tick, which already
    reads it all with CAP_DAC_READ_SEARCH. OPEN QUESTION 2. It restarts the terminator over
    D-Bus (units package, as it restarts the homeserver in step 2): the unit does not hide
    /run/dbus and allows AF_UNIX, so no change was needed for that.
  - IPAddressDeny=any / IPAddressAllow=localhost as the design, with a comment that a drop-in
    may add IPAddressAllow= lines (systemd appends them to the list) and the deny stays. The
    per-host drop-in S13 needs is NOT built (brief).
  - D 4.2's six paths (the design text names them for every unit, C 3.2.3 line 298-302).

teamchat-health.timer: C 3.11.1's three values; Description and [Install]
WantedBy=timers.target are mine (as backup.TimerUnit has them).

teamchat-backup.service/.timer: backup.ServiceUnit/TimerUnit unchanged (C 3.12.1).

resolv.conf: v1's files/usr/local/share/agent-bus/resolv.conf, reworded for v2 (it names
DESIGN-v2-C-host.md 3.2.3 and teamchat-server@.service; no v1 name in it).


4. Defence Before Fix
---------------------

"A unit text no gate judges": detector TestNoTeamchatUnitIsAStaticFile (v1names). It runs
`git ls-files -z` at the repository root (through execx.OS, in a test, with
-c safe.directory=<root> since the checkout may belong to another uid) and fails on any tracked
path with a component that is teamchat-<x>.<unit type> (service, socket, timer, path, mount,
automount, swap, target, slice, scope, device), with .j2 and a drop-in directory's .d taken off
first, naming each. Red on a real tracked fixture and on two kept fixtures (section 2); green on
the tree (no tracked file matches today). Goldens are named *.golden so they do not match. It
does not catch a unit text inlined in a YAML task's `content:` or in a non-Go script: those are
not files named for a unit. Whether that gap needs a detector is for the play's author (the
play's own read-as-YAML test is the natural place).

"Built and tested, reachable from no production entry point": unitfiles' functions are all
reached from cmd/teamchat-admin through `units list`/`units print` (Files, Names, Lookup)
except ExecBinaries, which only tests call today; it is the export the play test needs
(brief), so a `deadcode -test` run (tests as roots) does not flag it. The second half (every
ExecStart= binary is a destination the play installs) needs the play and is NOT built. What
it should do: for each unitfiles text and both host.DropIns texts, ExecBinaries(text) must
each be a `dest:` of a copy/link task in play-teamchat.yml; red today on /usr/local/bin/teamchat.

Could not confirm in this checkout that deadcode runs with -test: neither src-go/qa/go-qa.toml
nor scripts/ names deadcode; GoProjectRules.md says go-qa checks "dead code" (go-qa-ci's own
step, not configured here). If that step does not pass -test, ExecBinaries will be flagged
until the play test lands.


5. Commands run, with results
-----------------------------

From src-go/ unless said:
- gofmt -l ./internal ./cmd: empty.
- go build ./...: ok. go vet ./...: ok. go vet -tags busfailpoints ./internal/teamchat/...: ok.
- golangci-lint run ./internal/teamchat/... ./cmd/...: 0 issues; with --build-tags
  busfailpoints: 0 issues.
- go test -race -count=1 ./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...:
  exit 0, 73 packages ok.
- ./scripts/qa-changed.bash (repo root), run 1: go ok; teamchat-keeps-off-v1 FAILED on the
  goldens' v1 paths (fixed, section 1). Run 2 after the fix: see "Commits" below.
- As uid 1001 (go test -count=1 -exec 'setpriv --reuid=1001 --regid=1001 --clear-groups') over
  paths, unitfiles, v1names, admin: failures, all this container's own, none in new logic:
  paths: v1-names.txt unreadable, "open .: permission denied" (6 existing tests);
  unitfiles: TestEachTextIsItsGolden "open testdata: permission denied";
  v1names: TestNoTeamchatUnitIsAStaticFile and the existing TestEveryUnitTextSourceIsJudged
  "stat .: permission denied" (the working directory is unreadable to 1001);
  admin: existing TestOSProcess (Getwd) and TestLookupHome ("unknown userid 1001").
  Everything else passed as 1001, TestTheStaticUnitCheckReadsTheCheckout (git in a temp
  checkout) included.
- systemd-analyze verify (systemd 257) on the printed units with stub drop-ins: only "Command
  /usr/local/sbin/teamchat-admin is not executable" (not installed here).
- The built teamchat-admin: units list, units print resolv.conf, units print nope -> 64.


6. Not built, and why
---------------------

- The play (play-teamchat.yml), its install of /usr/local/bin/teamchat, and the ExecStart=
  destination test: another person's (brief).
- The per-host health drop-in with the teams' listen addresses (S13, decision 1): a later
  task (brief).
- The markdown edits (DESIGN 3.2.3, 3.3; GoProjectRules.md): blocked by #147, text in 7.
- No change to `team apply` (it still reads the installed base units' text as before).


7. Markdown edits for the coordinator to apply (blocked here by #147)
---------------------------------------------------------------------

7a. CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md, 3.2.3, replace

    (slice S7a), and `host.BaseSandbox` renders it; every v2 system unit carries it (the health and
    seats units, which Go does not yet make, with them).

with

    (slice S7a), and `host.BaseSandbox` renders it; every v2 system unit carries it (the seats unit,
    which Go does not yet make, with them). Go makes the text of every unit the play installs
    (package `unitfiles`: this template, the terminator's `teamchat-tls@`, the health and backup units
    and the resolver stub), and the play installs what `teamchat-admin units print` writes.

7b. Same file, 3.3: in the table, the "anyone else" row's verbs become
    `declare check`, `element check\|apply`, `status`, `version`, `units list`, `units print <name>`
(version and the units verbs are ClassAny in code; the row is where version already sits), and
before the paragraph that begins "`version` answers for every class" insert:

    `units list` and `units print <name>` answer for every class too: they read nothing and hold no
    secret, and the play runs them on the target as root (Plan 00246 INSTALL-PATH.md, stage A).
    `units list` writes one line per file the play installs, `<name><TAB><install path>`, in a fixed
    order (the homeserver's and the terminator's base units, the health service and timer, the backup
    service and timer, the resolver stub); `units print <name>` writes exactly that file's text, and a
    name that is not one of them is a usage error (64) naming them. So the play holds neither a unit
    text nor a path of its own.

7c. CLAUDE/GoProjectRules.md, "Defect classes that cannot be a rule", a new bullet:

    - **A unit text no gate judges** (Plan 00246 INSTALL-PATH.md, stage A). The unit gates of
      `internal/teamchat/v1names` (D 4.2's six paths, an allow paired with its deny) judge the
      texts Go makes, and `TestEveryUnitTextSourceIsJudged` finds every Go file that makes one; a
      unit written as a static file or a Jinja template is not Go, so no analyzer sees it. What
      holds it: `TestNoTeamchatUnitIsAStaticFile` fails on any tracked file named as a teamchat
      unit or drop-in (also as a `.j2`), red on a tracked fixture and on a fixture checkout; the
      play installs what `teamchat-admin units print` writes (package `unitfiles`). Not caught: a
      unit text inlined in a YAML `content:` or a script.


8. Open questions
-----------------

1. Before=lxc.service on teamchat-tls@ (section 3): right, or should the ordering be left to
   the homeserver alone?
2. The health unit writes all of /var/lib/teamchat-admin to reach the TLS directories. The
   narrower route is the same per-host drop-in decision 1 uses for the IP allow, adding
   ReadWritePaths= per team's TLS directory; that couples the leaf renewal to the drop-in being
   current. Owner's choice.
3. /run/teamchat-admin is a tmpfs path: after a reboot nothing creates it before the health
   timer's first tick (30 s), and a ReadWritePaths= entry that does not exist fails the unit's
   namespace setup (226/NAMESPACE). The backup unit has the same line. I did not change it (it
   is the design's text and the fix is the play's: a tmpfiles.d entry, which would be one more
   file for unitfiles to make, or a "-" prefix). Needs a decision before stage A goes live.
4. INSTALL-PATH.md says `deadcode -test` is in the Go gate; this checkout's config does not show
   it (section 4).


9. Tool faults met (dogfooding)
-------------------------------

- hooks daemon R-MARKDOWN-WRONG-LOCATION refuses every .md write in a nested sub-agent
  worktree: already open as #147; this file's .txt name is the result.
- The commit-time secret scanner reads the server template's golden file name (the unit name,
  an at sign, ".service.golden") as an email address; it refused this report too while it spelt it
  (a template unit name with a dot-suffix after the @), and rejected the commit; the
  plain `teamchat-server@.service` spelling passes. I changed the fixture to a non-template
  name instead of bypassing. Not filed (I cannot be sure it is not known): for the
  coordinator to log; it will hit any test or doc naming a template unit's file with a
  further suffix (.d/x.conf passed, .golden did not).
- qa-changed run 2 waited on another agent's QA lock for minutes with no estimate (#143 covers
  qa-changed's scope; the wait itself behaved as documented).
- `go-qa` dead-code configuration not visible in-repo (section 4).


Commits
-------
- db65a356f Plan 00246 Task 8.8: Go makes every unit text the install play puts on the bus
  server host (all the Go: paths, unitfiles, v1names tests, the admin verb).
- the commit adding this report (plan-only).
qa-changed run 2 on the code commit's tree: passed (go-qa both modules, teamchat-keeps-off-v1).
origin/F44 is fully contained (rev-list right-hand count 0) at push time.
