# Independent search: issue 247, three defect classes

Dispatch contained class descriptions and known instances only, no rule text. Not opened: src-go/qa/rules, src-go/qa/ansible, src-go/qa/internal, any detector config. No detector run. Paths are relative to the worktree root.

## Class 1: long-running unit with no [Install] (static unit)

Forms expected: Go string consts, concatenations and line lists; Python line lists; files/ unit files and .j2; inline content: in playbook YAML; drop-ins (a drop-in has no [Install] by design and is not a unit); golden/testdata copies.

### Carrying the hazard (confident)

1. src-go/internal/teamchat/unitfiles/unitfiles.go:34-46 (serverHead), :49-52 (restartLines), :62-66 (serverUnit) = teamchat-server@.service. Type=notify, Restart=always, no [Install]. Started and enabled by apply.go converge7 (apply.go:816-822). Nothing starts it at boot (no timer, no Wants= from an enabled unit; the tls unit only Wants= it and is itself static). Known instance.
2. src-go/internal/teamchat/unitfiles/unitfiles.go:86-114 (terminatorUnit) = teamchat-tls@.service. Restart=always (:112), no Type= (simple), no [Install]; Wants=/After= the server unit. Started and enabled by tlsunit.go:98-102. Known instance.
3. Golden copies of 1 and 2: src-go/internal/teamchat/unitfiles/testdata/teamchat-server@.service.golden and teamchat-tls@.service.golden. Same text, same absence; tests, not separate sources.

### Checked, not carrying the hazard

- unitfiles.go:122-149 healthService: oneshot, no [Install], no Restart=. Started by teamchat-health.timer (:152-162, [Install] WantedBy=timers.target). Play enables the timers: playbooks/imports/play-teamchat.yml:582-590.
- src-go/internal/teamchat/backup/units.go:10-19 TimerUnit ([Install] timers.target), :25-30 ServiceUnit (oneshot, timer-driven). Enabled at play-teamchat.yml:582-590.
- src-go/internal/teams/seatsweep/units.go:24-45: service oneshot, no Install; timer has [Install] WantedBy=timers.target.
- src-go/internal/teamchat/host/lxc/unit/unit.go:72-95: session unit, Type=forking, Restart=always, [Install] WantedBy=multi-user.target (also testdata/session.service). Wants symlink made at lxc/apply.go:1231.
- helpers/agent_bus_lxc/members.py:963-987: Type=forking, Restart=always, [Install] WantedBy=multi-user.target; enabled at :1260.
- files/etc/systemd/system/agent-bus-hs@.service: Type=notify, [Install] WantedBy=multi-user.target (:58). Its drop-in text, helpers/agent_bus/render.py:184-208 (render_dropin, Restart=always), is a drop-in, not a unit. Go drop-ins host/render.go:214-223 and host/tlsunit.go:33-58 are drop-ins for units 1 and 2 (the hazard is in the base unit).
- agent-bus-hs-health@.service, agent-bus-backup@.service (oneshot; timers have Install); abrt-prune-stale.service.j2, fedora-desktop-self-update.service (timer driven); fedora-desktop-self-update-verify.service.j2 (oneshot, [Install]); kernel-version-manager.service/.path ([Install]); lxc-docker-user-iptables-reconcile.service (oneshot, [Install]).
- files/etc/systemd/system/displaylink-dock-recovery.service: oneshot, no Install, started by udev/polkit; not long-running. The inline unit in playbooks/imports/optional/hardware-specific/play-displaylink.yml:364-374 has [Install] WantedBy=suspend targets.
- User units: ccy-sessions-restore.service ([Install]; Wants= ccy-sessions-set-going.service), ccy-sessions-set-going.service.j2 (Type=exec, no Install, pulled by that Wants=, deliberate), container-watch, container-images-prune, stt-watch, vmtest-nightly, host-health-collect services (oneshot, timer-driven, timers have Install), host-health.service.j2 ([Install] graphical-session), vmtest-bridge@.service and -heartbeat@.service (oneshot, started by .path/.timer with [Install]), stt-watch-failed.service (OnFailure= target), wsi-stream-server-at-login.service (Restart=no, [Install]).
- Inline YAML units: play-prevent-ssh-suspend.yml:36-43 (Restart=always, [Install] multi-user.target), play-rclone.yml:397-418 (Restart=on-failure, [Install] default.target), play-qobuz.yml:236-240 (rescrobbled, [Install] default.target, enabled at :305).
- Drop-ins only: play-network-wait-tuning.yml:67, play-speech-to-text.yml:100-102 (ydotoold override), src-go/internal/ftpcamera/server.go:286 (vsftpd override), restore/argv.go:50 (transient property list, Restart=no).

## Class 2: check-act-note pair whose guard the action cannot satisfy

Searched: Enable/Disable/Start/Stop/Restart through Systemd interfaces, UnitFileState, Enabled, is-enabled, "enable" strings in Go, Python helpers and bash installers.

### Carrying the hazard (confident)

1. src-go/internal/teamchat/host/apply.go:819-824 (converge7): st.Enabled false -> Systemd.Enable -> noteIf(err, "unit enabled"). Enabled comes from src-go/internal/teamchat/units/units.go:606-610 (UnitFileState == enabled or enabled-runtime). For a static unit (class 1 item 1) enable is a no-op, UnitFileState stays static, so the note repeats every run. The play counts the note as a change: play-teamchat.yml:578 (tc_applied changed_when on tc_changed_re over stdout lines). Known.
2. src-go/internal/teamchat/host/tlsunit.go:101-104 (convergeTLS): same pair, note "tls unit enabled", counted at play-teamchat.yml:578. Known.

Same root cause as class 1 items 1 and 2. The start/restart arms (apply.go:812-817, tlsunit.go:94-99) read ActiveState, which Start satisfies, so they converge.

### Related, unsure it is the same hazard

3. src-go/internal/teamchat/host/remove.go:109-113 (stop): if st.Enabled then Disable and note "disabled". Same Enabled read. For a static unit Enabled is false, so Disable is skipped (no repeating false change; the disable is just not attempted). Low confidence it carries this hazard.

### Class 2 checked, converge correctly

- src-go/internal/teamchat/host/lxc/apply.go:1208-1251 placeUnit: change decided by file write result, symlink result and recorded prior status, not by a systemd read the action cannot satisfy.
- apply.go:718-741 startForBackup: reads Active; Start satisfies it. remove.go:98-108 Stop guarded by Active; satisfied.
- helpers/agent_bus_lxc/members.py:1260-1262: UnitFileState != enabled -> enable -> emit "enabled". Same shape, but the unit has [Install] (members.py:985), so it converges. It would repeat if [Install] were lost.
- files/usr/local/sbin/agent-bus-install:752-758: is-enabled --quiet -> enable -> changed; hs service and both timers have [Install]. Converges.
- play-teamchat.yml:582-590: systemd_service enabled: on timers with [Install]; the module reports its own state.
- health/probe.go:190, host/leaf.go:274, host/rotate.go:74, restore/engine.go:350: Restart/Stop with no enabled-guarded note pair.
- sessions: src-go/internal/sessions/restore.go:111-125 restoreEnabled reads the wants symlink; read only.

## Class 3: scratch-directory tasks that report changed every pass

Searched all tempfile in playbooks/ and tasks/ (4 tasks), then tasks whose path is under a registered tempfile dir or a var passed into an imported file, plus fixed /tmp scratch paths and mktemp.

### Carrying the hazard (confident): all in the play-teamchat.yml block at lines 254-383, which has no when:, so it runs on a converged machine every pass

1. playbooks/imports/play-teamchat.yml:256-264 'Make the Controller's Build Directory' (tempfile, delegate localhost). No changed_when. Known.
2. tasks/teamchat-build-go-commands.yml:32-40 'Make the Cross Build Directory for Each Architecture' (file state=directory under go_cross_dir, which is tc_build_dir.path, passed at play-teamchat.yml:304). No changed_when; the dir is new each run. Known.
3. playbooks/imports/play-teamchat.yml:375-383 'Remove the Controller's Build Directory' (file state=absent, in always:). No changed_when; changed whenever the dir existed, i.e. every pass. Known.

### Same family, not carrying the hazard

- tasks/teamchat-build-go-commands.yml:42-61 build command: changed_when: false (:61).
- play-teamchat.yml:308-316 stat (never changes); :318-332 copy to digest-named home (changes only when bytes differ).
- playbooks/imports/play-agent-bus.yml:328-333 tempfile changed_when: false; :335-345 copy into it changed_when: false; :359-365 file absent changed_when: false.
- playbooks/imports/play-toolbox-install.yml:71-76 tempfile changed_when: false; get_url :78-88 and unarchive :90-94 into it have no changed_when but sit in a block gated by when: not st_jetbrains_toolbox.stat.exists and has_display (:115), so run only on real work; :117-121 remove changed_when: false.
- play-teamchat.yml:431-435 tempfile, get_url :437-443, copy :451, always-remove :468-472 (none with changed_when): the block is under when: (:426-429), true only when the Tuwunel binary or record is missing or out of pin. Real work only. :446 unpack changed_when: false.
- Fixed /tmp scratch (outside the stated class, not tempfile): play-python.yml:247-250 file absent on a /tmp golangci archive, ungated but reports ok when the file is absent, so converges (download :220 and unarchive :230 are gated); play-photography.yml:178-200 all gated on a version mismatch.

## Attribution by technique

Text search found: all unit-text sites ([Service], ExecStart=, Restart=, WantedBy, [Install]), the 4 tempfile tasks, the Enable/Disable call sites, the go_cross_dir uses.

Reading found what text search could not: that serverHead and restartLines assemble the server unit from constants and strings.Join, and that no [Install] exists in it (an absence grep cannot show); that the TLS unit is assembled in a loop; that Enabled derives from UnitFileState (units.go:610); that tc_applied counts the "unit enabled" note as changed (play-teamchat.yml:578); that the toolbox and Tuwunel blocks are gated by when:; that the build task carries changed_when: false; that lxc placeUnit and members.py converge; that render.py output is a drop-in.

## Searches run

- grep -rIl for [Service], [Unit], ExecStart=, Restart=, WantedBy over src-go, scripts, tasks, playbooks, helpers, docs, files; then a WantedBy/RequiredBy file list. The first pass wrongly excluded src-go/internal through an --exclude-dir=internal and was redone.
- grep with line numbers for Restart=, Type=, [Install], WantedBy, RequiredBy in files, helpers, playbooks, tasks, scripts, src-go non-test; [Service] and ExecStart= in yml, j2, py, go.
- grep for Enable, Disable, Start, Stop, Restart, Mask, Reload calls in Go; "enable", "disable", is-enabled, --now, is-active; UnitFileState and Enabled; is-enabled, UnitFileState, enable in helpers and files/usr.
- grep for tempfile in playbooks and tasks; teamchat-build-go-commands and go_cross_dir; scratch-path patterns (/tmp, tmp, temp, scratch, build_dir, work_dir, download_dir, staging variables), mktemp and TemporaryDirectory.
- Read: unitfiles.go, apply.go (715-845), tlsunit.go, remove.go, lxc/apply.go (1205-1251), members.py, agent-bus-install (735-760), render.py, the four tempfile play sections, tasks/teamchat-build-go-commands.yml, play-teamchat.yml (245-600), play-python.yml, play-photography.yml.

Saturation: the last searches added nothing new. Not checked: unit text assembled at run time from templates outside these trees, and tests/ fixtures.
