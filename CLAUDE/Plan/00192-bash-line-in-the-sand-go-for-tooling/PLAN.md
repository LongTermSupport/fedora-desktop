# Plan 00192: Line in the sand: tooling is Go, bash only for small helpers

**Status**: In Progress. Every QA script is ported or retired (`qa-go.bash` by Plan 00191
Task 3.3; the Go gate is go-qa-ci through `scripts/qa-go-qa.bash`). Left for workers: the
remaining ports of Task 3.3 (the `ccy` launcher's phases 3 to 5 are deferred by Plan 00191
D4/D10), and Tasks 4.1 and 4.2 over the finished plan. Left for the host agent: the host checks
of the merged ports still open (vmtest H3's base build, once the runs are pruned; open H5's
Ctrl-C in mpv; rclone-tail H4; gh-account-setup H1 and H3 to H5); Task 3.2's root run is on hold
until Plan 00209 Task 2.5 (see the task). Left for the owner: only what needs the owner in person, namely
gh-account-setup H2 (a real GitHub device login), ftp-camera H3 and H4 (the physical camera),
H1 (it sorts the real upload tree, so it waits on the owner's yes) and H5 (a Wi-Fi access
point on the owner's machine), wsi H1 (speaking into the microphone) and H2 (a real recording
and Escape on the owner's desktop), open H2 and H3 and reclaim H5 (the owner's screen), and
vmtest H6 (a throwaway GitHub account to create).
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

The owner's decision (2026-10-14): no more tooling written in bash. Plan 00191 ported
`ccy-teams` and `ccy-sessions` to Go and showed why: bash binds names when a line runs, types
nothing, and its interactive tools can only be tested through a pseudo-terminal. From now on
bash is for very small helpers only; anything over a measured threshold is written in Go in
`src-go/`. The rule is written into the root `CLAUDE.md` and `README.md` and enforced by a QA
gate, with the existing large scripts on a ratchet allowlist that may only shrink.

The existing bash (and then the standalone Python the Go tools call) is inventoried and the
large tools are rewritten in Go, one feature branch per programme. The owner's method
(2026-10-14), from qa-ansible on: a spec of intended behaviour as clear expectations, each a
test written first; the bash is read for requirements and never treated as correct, and no
replay of it is recorded (src-go/CLAUDE.md, "How a port is built"). The earlier ports' replays
stay as regression tests.

go-qa-ci (Plan 00191 Phase 3) is reviewed for completeness, and this repository starts
recording its own Defence Before Fix rules for the defect classes the ports found.

## Goals

- The rule, with pragmatic thresholds, in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md`
  linked from the root `CLAUDE.md`.
- A QA gate in `qa-all.bash` and `qa-changed.bash` that fails a new bash file over the
  thresholds and an allowlisted file that grows; the allowlist only shrinks.
- An inventory of bash and Python with a ranked list of rewrite programmes.
- Each major rewrite on its own feature branch, merged after full QA and a review.
- Project DBF rules for the defect classes found in the ports.
- A Fable adversarial review of the whole and its findings fixed.

## Non-Goals

- Python that Ansible loads in-process (plugins, filters, modules, callbacks) or that imports
  Ansible: it stays Python.
- Bash that must run before Go exists (the bootstrap) or is sourced into a user's shell.
- The `ccy` launcher's port: assessed in Plan 00191 (`ASSESS-ccy-launcher.md`). Its decisions
  D1-D10 were defaulted by the coordinator there (Plan 00191 Task 4.2); phases 0 to 2 are built
  in that plan and phases 3 to 5 are deferred by its D4/D10.

## Tasks

### Phase 1: The rule

- [x] ✅ **Task 1.1**: Inventory of tracked bash and Python with sizes, invokers, tests and a
  port/keep class: [INVENTORY.md](INVENTORY.md).
- [x] ✅ **Task 1.2**: Thresholds chosen from the inventory's counts (60 code lines, 90
  statements, 4 functions, no associative arrays, no interactive UI, no data parsing past one `jq -r`); the
  rule in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md` linked from the root `CLAUDE.md`.
- [x] ✅ **Task 1.3**: The gate, in Go (`src-go/qa/cmd/qa-bash-limits`, `src-go/qa/bashlimits`), with its
  ratchet allowlist `scripts/bash-limits.allowlist` (records only move down; `--base` refuses
  an added entry or a raised record), wired into `qa-all.bash`, `qa-changed.bash` and CI;
  controls (over-long, too many functions, `declare -A`, `fzf`, growth, an added entry) fail.

### Phase 2: DBF rules and go-qa-ci

- [x] ✅ **Task 2.1**: go-qa-ci audited: [AUDIT-go-qa-ci.md](AUDIT-go-qa-ci.md). Complete with
  recorded gaps; its bundled rule set is one analyzer; this repo had no project rules.
- [x] ✅ **Task 2.2**: Project DBF rules as `go/analysis` analyzers in the nested module
  `src-go/qa` (so the shipped binaries stay dependency-free), run by the Go gate (now `qa-go-qa.bash`): one process
  seam, owned programs (sudo, tmux, gum, fzf, systemctl, git), tmux target forms, no wall clock in
  tests, one home abbreviation, a `WaitDelay` that names its error (six rules); catalogue [GoProjectRules.md](../../GoProjectRules.md). Each red on a fixture of
  the historical defect, green on the tree after fixing the real instances; on landing they
  caught the bash-limits gate running git outside `gitx`.

### Phase 3: Rewrites, one feature branch each

- [x] ✅ **Task 3.1**: Programmes from the inventory, ranked; each its own branch.
  `go-port/freeze` (P7), podfreeze and lxcfreeze: merged into F44 and pushed (`f0284848`). Spec
  [SPEC-freeze.md](SPEC-freeze.md); the library (`internal/freeze`), the two engines and the
  shared flow in Go; golden replay of the bash tools' recorded output
  (`internal/freeze/golden`); one task file builds any Go command
  (`tasks/build-go-command.yml`); the bash tools, library, suites and gates deleted. The Fable
  review's fixes are in (sudo asks on `/dev/tty`, Ctrl-C and a lapsed sudo timestamp end an act
  batch cleanly, the sudo `-n true` probe, the inventory generic over the engine's record).
  Host acceptance: deployed by `meta-deploy.bash`. Output parity with the bash tools is
  established: the first run matched byte for byte on three of the four commands (the fourth
  stopped on a sudo prompt the Go sudo probe raised wrongly, now fixed), and the second run,
  with no prompt, differed only by the containers started between the runs, every common row
  identical. A row comparison against a saved baseline cannot repeat on a machine whose
  containers change, so `acceptance.bash` now checks the layout only (Task 3.4). Left, for the host agent:
  [ACCEPTANCE-freeze.md](ACCEPTANCE-freeze.md) step 2 (freeze and thaw a throwaway container
  with each tool; the menu driven by tmux keys and read with `capture-pane`; Ctrl-C mid-batch
  gives 130; `lxcfreeze` needs sudo at the run) and step 3 (the meta-deploy run queued for
  00192: the freeze build and install tasks report ok, not changed, read from the play recaps,
  and the `acceptance.bash` leg passes). Step 2, partial (bus `$8aqX_PoK5b62YkY5FNbJF0kZ2SBsP8XxhoXIrIR5fkI`):
  podfreeze freeze/thaw and the tmux-driven menu PASS on throwaways. `lxcfreeze` PASS 26-10-26
  (host agent, Alpine and Rocky throwaways: freeze, thaw, list, the fzf menu, raw `lxc-freeze`
  thawed by the tool); the thaw without NetworkManager exits 1 (Task 3.5). The rest PASS
  27-10-27 (host agent, throwaways): Ctrl-C after the first `✓` gives "interrupted — 1 of N
  container(s) done, the rest were not touched.", exit 130, in both tools; the numbered menu
  with no fzf on `PATH` lists the groups, drills in, freezes and thaws all, `q` exits 0. Sudo
  asking again after its timestamp lapses cannot occur on this host (its sudo needs no
  password, so it keeps no timestamp); that path rests on the Fable fix's unit tests. Step 3
  PASS at F44 5340d3ac7 (meta-deploy, META_RC=0): every freeze build and install task `ok`,
  both plays changed=0, and `acceptance.bash` COVERAGE 11 of 11, the layout kept for `list` and
  `freeze --all -n` of both tools.

- [ ] ⏸️ **Task 3.2**: `agent-bus-install` on `go-port/agent-bus-install`: spec
  `SPEC-agent-bus-install.md` on the branch; built, reviewed and its review fixed, not merged.
  It runs as root and changes behaviour (its D7), so it would merge only after a host test as
  root (the branch's `ACCEPTANCE-agent-bus-install.md`). **On hold, no host run** (checked
  27-10-27 against Plan 00209, report `subagent-reports/261027-task-3-4-3-2.md`):

  - v2 does not port this installer, it replaces it: `teamchat-admin` (00209 Tasks 2.1-2.3) is
    v2's own root installer, designed from requirements, citing this spec's bug list as `I-B<n>`
    (DESIGN-v2-C), so the spec's value is already carried. v1, and this installer with it, runs
    beside v2 until every team has migrated (Phase 6, owner-paced); retiring v1 is a later plan
    (Task 6.3). No task of 00209's Phases 1 to 6 is started yet.
  - The branch is stale: last touched 26-10-14; `git merge-tree` with F44 gives 24 conflicts (at 4a4f1c634),
    the bash installer itself among them (modify/delete). F44 changed the bash installer in 7
    commits (78 lines: the stamps group, health, backup timer state, the Go status line) and its
    suite by 115 lines since; none of that is in the port, so a host test now would test an
    installer that no longer matches v1.
  - It changes the installer every running v1 team depends on (D7) while 00209's migration
    calls v1's verbs as they are (`backup-now`, `remove`), and the bash file is held by the
    ratchet, so leaving it in bash costs nothing until it must grow.
  - **Decision: park, do not drop.** Re-decide when 00209 Task 2.5 passes (`teamchat-admin`
    accepted on the host): drop the port then if v2 stands. Revive it earlier only if the v1
    installer needs a change the ratchet refuses, or 00209 is stopped. Reviving means merging
    F44 into the branch, porting those 7 commits test first, a review, then the host run. The
    branch stays, unmerged.

- [ ] 🔄 **Task 3.3**: The rest of the inventory's programmes, ranked there; the `ccy`
  launcher's port is Plan 00191's (D1-D10 defaulted there, phases 0 to 2 built, 3 to 5
  deferred by its D4/D10). Each port: a spec first, its tests first, a Fable review whose
  findings are fixed, then merged. Per-port detail, review dispositions and departures:
  [PORTS.md](PORTS.md). Status by port:

  - Done, merged into F44: `qa-changed`, `qa-all`, `qa-ansible`, `qa-nokill-containerwatch`,
    `qa-patterns`, `qa-python`, `qa-ansible-syntax`, `qa-bash`, `qa-discovery`,
    `qa-deployed-drift`, `qa-js`, `qa-ansible-pause`, `qa-gh-scopes`, `qa-ready-wait-rules`,
    `qa-plan-script-logging`, `qa-toolchain`, `qa-speech-to-text-rules`, `qa-version-pins`,
    `qa-vmtest-manifest`, `qa-helper-tests`, `qa-docs`, the shared `qa/internal` scaffolding.
  - Not ported by decision: `qa-go.bash` (retired by Plan 00191 Task 3.3; go-qa-ci replaces it).
  - Done, merged, host checks left: `vmtest` (P5, SPEC-vmtest H1-H6, the host agent's on the
    KVM host), `ftp-camera` (P8, SPEC-ftp-camera: H1, H2 and H5 the host agent's; H3 and H4
    the owner's, since they need the physical camera uploading, though H4's "geeqie shows the
    frame" is a screenshot the host agent can read once a file arrives), `watermark` (H3, the
    host agent's) and `wsi` (H2 the host agent's; H1's live dictation and audible grace the
    owner's, with the transcription half provable by a recorded fixture).
    H3's wording was wrong and is corrected: a second run on the same input exits 2 (the output
    exists, M10), and exit 3 is a run on the watermarked output (M9), as the host showed. wsi H2
    now also checks that the recording is gone after Escape (W15a, Plan 00171 Task 5.12).
    Host results, 2026-10-23: vmtest H1 and H5 PASS, H2 PASS apart from its bridge half,
    H4 PASS (`$J1MsGTTA4klOto2BoOTEamrIfElxFFz2HuwHLDHUVmE`, `$8aqX_PoK5b62YkY5FNbJF0kZ2SBsP8XxhoXIrIR5fkI`),
    H4's Ctrl-C during member-up PASS, exit 130, no guest left
    (`$49WVO-g6jUYPrUTWNVTGt_VLL2bk3WD7ydjiJVHMzMY`); left: H2's bridge run, H3 and H6, and
    H7 (`prune-runs`, Task 3.10).
    watermark H3 PASS (`$NIHwpSi3rTN-Z2LSvujaLWtueWfi_dZedKS4rkYtB9M`), the source of the
    wording correction above.
    Host, 2026-10-27 (host agent; `$k9uI3FUf_DcvOv_JwAhf17IurP3meue4j1dnETUvOGY`,
    `$TI8EqLDcYBMAKoH3ZkGnXJtHBgW6qUvqNluei5bMPXM`): vmtest H2's bridge run PASS
    (`server-fast-provision` through `vmtest-request.bash`, 13 of 13 checks, `vmtest verify`
    signature ok, verdict pass); H3's run half PASS (`desktop-fresh-install` 16 of 16), its
    base build (`vmtest build-base desktop-44`) not started: the host's root filesystem was at
    99%. Finding: the kept overlays of failed runs under the lab's runs directory had grown to
    about 108G in 37 runs, never removed, and vmtest has no verb to prune them; being fixed on
    `fix/00192-vmtest-prune-runs`. H6 SKIP, it needs a throwaway GitHub account. ftp-camera H2
    PASS (`--copy-preflight` printed the rclone RC address, exit 0); H1 and H5 not run (the
    owner's, above). wsi H2 not run (the owner's).
  - `open` (P6): done, `go-port/open` and `go-port/open-2` both merged into F44 and deployed
    (`8c2de08c`); its host checks H1, H4 and H5 are the host agent's (terminal steps through
    tmux), H2 and H3 the owner's (the desktop), not queued in `meta-deploy.list`. Spec
    [SPEC-open.md](SPEC-open.md). Host, 2026-10-23 (`$NIHwpSi3rTN-Z2LSvujaLWtueWfi_dZedKS4rkYtB9M`):
    H1 and H4 PASS (H4's image render unproven by `capture-pane`); H5 partial (Ctrl-C in mpv
    not run, mpv was then missing; it is now installed, mpv v0.41.0 by `play-cli-tools.yml`,
    `$SaaNjb_VFudX-vnN6tjA8rXQXtbNv_-y_dJUJgXxFSA`); H2 and H3 (the owner's screen) not run.
    Host, 2026-10-27 (host agent, a private tmux, `$k9uI3FUf_DcvOv_JwAhf17IurP3meue4j1dnETUvOGY`):
    H1 PASS; H4 PASS for text (the editor), a directory and `.` (`ls` in `less`), a PDF
    (`pdftotext` in `less`) and an unknown type (the chooser, Esc exit 130). H4's image: `chafa`
    ran, exit 0, but drew nothing inside the nested tmux (`TERM=tmux-256color`), while
    `chafa --format symbols` drew text; fixed (E11a), and the rerun at `071969780` in the same
    tmux drew the image in text symbols, exit 0 (`$WJ4Fgyz5sHTT3KCDHHKiNF8P6i4Z8PLlx9f7rcZS2Wk`). H5: Ctrl-C
    in `less` is less's, `q` exits 0 with the terminal normal; a named pipe is classified
    without blocking, and Ctrl-C at the chooser gives `cancelled — nothing opened.`, exit 130
    (E31). Not run: Ctrl-C in mpv (left, above), Ctrl-C ending chafa (a still image returns at
    once) and Ctrl-C during a slow file (no slow file found). H2 and H3 are the owner's.
  - `rclone-cache-status` (P6): ported, merged into F44, deployed and live-checked on the host
    (`deploy-rclone-cache-status.bash` passed), spec
    [SPEC-rclone-cache-status.md](SPEC-rclone-cache-status.md); left, for the host agent: the
    checks inside a mount (H2, H3) and `rclone-cache-warm`. Host, 2026-10-23: H1, H2 and H3
    PASS, `rclone-cache-warm` brackets its run (`$NIHwpSi3rTN-Z2LSvujaLWtueWfi_dZedKS4rkYtB9M`).
    H3's wording "`rclone-tail --once` still brackets" is wrong against the code: only the
    streaming mode brackets (`$SaaNjb_VFudX-vnN6tjA8rXQXtbNv_-y_dJUJgXxFSA`); the spec needs
    that correction.
  - `rclone-tail` (P6): ported, cut over and merged into F44 at 1d8bc1037; spec
    [SPEC-rclone-tail.md](SPEC-rclone-tail.md). Its host run `deploy-rclone-tail.bash` (H1
    scripted by `acceptance-rclone-tail.bash`) PASSED through meta-deploy at b1e298e1c, 5 of 5
    checks, `~/.local/bin/rclone-tail` now an ELF (`$SaaNjb_VFudX-vnN6tjA8rXQXtbNv_-y_dJUJgXxFSA`); H2 to H4 are
    the host agent's (a file written into a mount while `rclone-tail` runs in a tmux pane,
    Ctrl-C by `send-keys`, `ftp-camera`'s closing status). H2 and H3 PASS 26-10-26 (host agent,
    a copy and delete into a `<remote>` mount: queue and rate during the upload, then idle;
    Ctrl-C exit 0); one overwrite showed two uploads, cause not found in the log. H4 left.
  - `gh-account-setup` (P6): ported and merged into F44 at 9e404ae6b (Opus MERGE in round 4;
    `src-go/cmd/gh-account-setup`); it is run from the checkout, so no play deploys it. Spec
    [SPEC-gh-account-setup.md](SPEC-gh-account-setup.md). Host checks: H1, H3 and H5 the host
    agent's, H4 in the VM lab (host agent); H5's Ctrl-C mid-login leaves the host's `gh`
    accounts as it found them. H2 is the owner's (completing a GitHub device-code login for a
    real account; the script side up to the printed URL is the host agent's).
  - `setup-rclone` (P6): ported (`go-port/setup-rclone-3`, the review's FIX-FIRST findings
    fixed) and merged into F44 (`61b70d02d`); spec [SPEC-setup-rclone.md](SPEC-setup-rclone.md).
    Run from the checkout through the `scripts/setup-rclone.bash` wrapper, so no play deploys
    it and nothing is queued in `meta-deploy.list`. Host checks H1-H4 are the owner's, at the
    desktop (a real remote and the vault).
  - `reclaim` (P6): built, Fable-reviewed and its findings fixed, merged into F44
    (`2e32edc2a`); spec
    [SPEC-reclaim.md](SPEC-reclaim.md), oracle the scenario table run against the Go and the
    bash ([PORTS.md](PORTS.md)). `play-cli-tools.yml` builds it. Host checks on 2.0.0 (host
    agent): H1-H4 pass; H5 (baobab on the desktop, by screenshot) is the owner's, since the
    host agent may open no window and take no screenshot. Two limits of that run: H3's "no
    sudo question" (B12) was not exercised, because the host user's sudo needs no password,
    and H4's option 6 read the scratch home's empty podman storage, so its podman preview was
    not seen against real data. H1 found two faults, fixed in 2.0.1: `du`'s ~95 raw
    `Permission denied` lines under `/var/log` and `/var/cache` (now one line per directory,
    counted, the first named; E6), and `podman system df` exiting 125 on a volume whose data a
    container's uid owns, so podman storage was never reported (now read again under
    `podman unshare`, the refusal named; E7, E22). 2.0.1 on the host: the `du` fix holds, but
    podman storage was still unavailable, because one rootless volume's data is owned by a uid
    outside the user's current subordinate range, which no user namespace maps. 2.0.2 reports
    podman storage in part from the listings that open no volume, names that volume and why
    (found from its owner and `/etc/subuid`), and option 6 then offers only the image prune
    (E7a, E22a). 2.0.2 on the host: the wording holds, but the partial section printed the
    raw listings (hundreds of lines), and option 6's image prune (`-af`) would have removed
    every unused tagged image, the per-project claude-yolo images included. 2.0.3 summarises
    the partial section (counts, sizes, dangling images, the largest ten of each kind), makes
    every default prune dangling-only, and asks separately, default No, about every unused
    image, listing the claude-yolo images it would remove; the safe sweep never asks it (E7a,
    E22, E22a, E27, B16). 2.0.3's host check is queued in `meta-deploy.list`. 2.0.4 drops the summed image size (it equalled the whole disk; no layer sizes are available without `podman system df`) and explains the dangling count that differs between the report and the prune question (images a container uses are kept); its host recheck passed at `cad6b5053`. 2.0.5 prints `Skipped <command>.` after a No to either "Also remove all unused" question as well; its host recheck passed at `071969780` (option 6, No to all four: a Skipped line after each, nothing run, podman and docker figures unchanged).
  - The `ssh-with-key` family (P6): built on `go-port/ssh-with-key`, review pending, not
    merged; spec [SPEC-ssh-with-key.md](SPEC-ssh-with-key.md) (D1-D5, E1-E19, B1-B10, H1-H5),
    detail in [PORTS.md](PORTS.md). `play-basic-configs.yml` builds the three commands. H1-H4
    are the host agent's (a throwaway key against a VM-lab guest); H5 (the owner's hosts, and
    whether E11's refusal stands for a configuration with a global IdentityFile) is the owner's.
  - Still to do: the remaining programmes in [INVENTORY.md](INVENTORY.md) (`nord`,
    `nvidia-status`) and the `ccy` launcher.

- [x] ✅ **Task 3.4**: Freeze step 3's `acceptance.bash` compared live engine output with the
  baseline saved at the first deploy, so it failed (7 of 11) once containers started or stopped:
  every diff was added rows only. Kept and fixed, not retired: the golden replay runs under
  fakes, so this is still the only check that the parsers read a real engine, and step 3 needs a
  scripted verdict. `freeze-layout.bash` compares the layout: container rows (the lines under a
  `  NAME` header) and counts dropped, every remaining line one the bash tool printed (the
  baseline, or the golden for an empty machine and a full dry run). The dry run may exit 1 with
  empty stdout. Baseline rows as a subset was rejected: a removed container fails it. Test first:
  `test-freeze-layout.bash`, 10 fixture cases (rows added, removed and thawed, a machine now
  empty pass; a changed header, a renamed heading, a stray stdout line, a wrong verb fail).
  Host check: step 3 PASS at F44 5340d3ac7 (COVERAGE 11 of 11; Task 3.1).

- [x] ✅ **Task 3.5**: `lxcfreeze thaw` on a container without NetworkManager exits 1 after a
  thaw that succeeded (SPEC-freeze bug 19, D6). Owner decision 26-10-26: no `nmcli` is not
  applicable, not a failure. Built: lxc-attach's `Failed to exec "nmcli"` (No such file)
  becomes `freeze.DoneError`, a `✓` with its note; every other nmcli failure still fails. No
  golden scenario pinned that message, so the golden is unchanged. Host check passed
  26-10-26 (Alpine throwaway, two cycles, `✓` and exit 0). The host's nit, that the freeze
  note still promised a renewal on every thaw, is SPEC-freeze bug 29: the note says "where
  NetworkManager runs inside", pinned on the 12 lxc scenarios that print it.

- [x] ✅ **Task 3.6**: `run.bash --changed` did not count `play-podfreeze.yml` and
  `play-lxcfreeze.yml` stale after their Go (`internal/ui`) changed: the staleness misses Go
  dependencies (Plan 00214's area). They were queued by name instead. Fixed on
  `fix/00192-changed-go-deps`: no Go-building play had any of src-go as an input, because
  `tasks/build-go-command.yml` reaches it as `root_dir ~ '/src-go'`. The mapper
  (`helpers/self_update/affected_plays.py`) now follows `root_dir ~`/`+` joins and takes a Go
  module root as the builds of the play's `go_command_name` commands (a `{{ var }}` name
  resolved through the walked files): every package each imports, transitively, in src-go and
  its vendor tree, read from the source and held to `go list -deps` by a real-repo test (24
  commands, 12 plays). Defence Before Fix rule `play-input-dropped`
  ([QA.md](../../QA.md#play-input-dropped)): 15 findings red, 0 green. Host check: the queued
  `changed-plays` entry lists the Go-building plays whose packages changed since they last ran.
  Report `subagent-reports/261026-task-3-6.md`. Host, 27-10-26 at f9073ce05: `changed-plays`
  ran 49 of 49 (most for Plan 00228's play edits), every Go-building play among them, and
  lxcfreeze, which the stale list before the run (play files only) did not name; afterwards
  nothing is stale. Its reasons never name a src-go package: Task 3.8.

- [x] ✅ **Task 3.7**: Inputs the play text cannot show, found by Task 3.6's independent search
  and outside `play-input-dropped`: `helpers/github_scopes/cli.py` reads
  `vars/github-required-scopes.yml` through `__file__`, so `play-github-cli-multi.yml` does not
  rerun when the required scopes change (class: a helper the play runs reads a checkout file
  outside its package); `environment/localhost/group_vars/desktop.yml` is an input of no play
  (coordinator default of 27-10-27, changeable by the owner, questions-room Q3: not counted;
  a later change may name the plays that read each changed variable); `scripts/qa-toolchain.bash`, run by
  `play-python.yml` as a check, reads `src-go/qa/` and `src-go/go.mod`, which are not inputs.
  Fixed on `fix/00192-hidden-play-inputs` (DBF): the rule `play-program-read-dropped`
  (`CLAUDE/QA.md`), red on 30 findings in 9 plays, green after. The mapper counts what the
  programs a play runs read: a helper's `__file__` paths and literal checkout paths
  (`helpers/self_update/not-play-inputs.json` lists the names that are not reads), a bash input's sourced library, and the
  Go command it builds with `run_go_tool`/`run_go_command`, with the checkout files that names.
  Report `subagent-reports/261027-task-3-7.md`. Merged into F44 (daf53f21c); the helpers run
  from the checkout, so no host run is needed. Helpers are judged a package at a time, so the
  gnome plays rerun when `vars/fedora-version.yml` changes though no module they run reads it:
  it errs towards a rerun, never a miss (coordinator default, accepted).

- [x] ✅ **Task 3.8**: `run.bash --changed` gives a play made stale by Go packages alone no
  reason a person can read: its reasons list names play files and `play-golang.yml`, never the
  src-go package path (host, 27-10-26). Name the changed package(s), or the command they build,
  in the reason, test first. Fixed on `fix/00192-changed-reasons-go`: the mapper records which
  commands each Go package is built into, the judgement keeps the changed packages, and one
  formatter (`changed_plays.reasons`) gives a Go change its own line, up to three packages and
  three commands (`changed Go packages: src-go/internal/ui (built into podfreeze)`). The login
  report and the panel read that formatter; `run.bash --changed` had printed no reasons at all,
  and now its helper writes one line per play to stderr before the list (run.bash unchanged).
  The login report already used the same judgement; the stale list the host saw names no Go
  path, which only the pre-3.6 mapper gives, so it was collected (hourly timer) before the merge. Report `subagent-reports/261027-task-3-8.md`. Host check: the next
  `run.bash --changed` after a Go-only change names the package and its command for each play.
  Host, 27-10-27 at d247e4b2f: before the run and in `--changed`'s own output, claude-yolo's
  reason read "changed Go packages: src-go/internal/teams/choose, …/hot, …/roles (built into
  ccy-teams)" beside its other paths. No play was stale from Go alone that run, so the Go line's
  own place was shown, not the Go-alone case; the formatter's tests cover that.

- [x] ✅ **Task 3.9**: `qa-changed` missed a suite a change broke. Task 3.7 made
  `helpers/self_update/affected_plays.py` load `not-play-inputs.json` at import, and
  `scripts/test-self-update-cycle.bash`, whose fixture copied `helpers/self_update/*.py` and
  `unattended-plays.json` only, failed 125 checks in qa-all; qa-changed had passed on the branch.
  A suite is chosen for a changed file when it names the path, and a glob (`*.py`) names none.
  The fixture is fixed (it copies `*.json` too, 218 pass); the gate gap is open: a suite that
  copies or reads files through a glob must be chosen when any file the glob matches changes,
  test first, with a Defence Before Fix sweep for other suites that reach files only by glob.
  Done: `src-go/qa/changed/reach.go` reads each suite's repository-rooted variables and the
  globs and directories it reaches through them; a changed file a glob matches, or one under a
  reached directory, selects the suite ("reaches F through P"), red then green on exactly the
  self-update case. Over every tracked non-markdown file no suite lost a selection and eleven
  gained 375 (subagent-reports/261027-task-3-9.md lists them).

- [ ] 🔄 **Task 3.10**: The VM lab's runs filled the host's disk: `vmtest sweep` keeps every run
  that did not pass, with its overlay (up to the 20 GiB guest disk), for ever, and vmtest had no
  way to remove one. Code done (SPEC-vmtest E61-E65, H7): `vmtest prune-runs`, a dry run unless
  `--yes`, removes only the `*.qcow2` of runs that passed, were followed by a passing run of the
  same scenario, or are older than `--older-than` days (default 7); the rule is retention.py's
  new `prune-plan`, beside the sweep's `run_passed`. A run whose domain libvirt lists, or that
  holds the new `run.lock` (taken from its directory to the end of its teardown), is live and
  left. `run` and `member-up` refuse with under 22 GiB free and a base build with under 26 GiB, before anything
  else, naming what `prune-runs` would free; a failed run says the size of the overlay it kept.
  H7 PASS at `886288d70` (`$kRFFD7XyKl4N7zP_QpP9m39uVvM3NxHI55Kcb0HwOm0`): the dry run listed
  15 of 37 runs (a later pass of the scenario, or failed and older than 7 days), the three
  bases untouched, "would free 107.5 GiB", and removed nothing. Open: `--yes`, the owner's call.

### Phase 4: Review

- [ ] 🔄 **Task 4.1**: Fable adversarial review of the rule, gate, rules and rewrites; findings
  fixed. Three reviews (subagent-reports/261014-fable-review-{gate,go,gqaci}.md) found
  blockers in each: the gate is evadable and its CI ratchet bypassable; the DBF rules caught
  1 of 13 planted evasions and Ctrl-C can wedge the terminal; go-qa-ci cannot be adopted (no
  nested-module driver, disabled linters counted as suppressions) and ships one rule. Fixing in
  parallel: the gate on a real bash parser with a versioned ratchet; type-based rules; runtime
  correctness; go-qa-ci M15 (adoption, gate) and M16 (a bundled rule pack). All five are
  merged (go-qa-ci's on its own remote), and so are typed state, the ccy-sessions and
  ccy-teams redraw cost, and the `teams` package split. The ports get their own review before
  merge: freeze's found two blockers, fixed and merged; qa-changed's and qa-all's findings are
  fixed; agent-bus-install's are fixed on its branch.
- [ ] 🔄 **Task 4.2**: qa-reviewer over the plan: done once
  ([subagent-reports/261014-qa-reviewer-plan.md](subagent-reports/261014-qa-reviewer-plan.md));
  its fixes are on `go-port/qa-all` ([the fix report](subagent-reports/261014-qa-reviewer-fixes-sonnet.md)).
  Runs again over the finished plan.

## Success Criteria

- [ ] A new bash file over the thresholds fails QA; an allowlisted one that grows fails QA.
- [ ] Every port in Phase 3 passes its existing suite against the Go binary before cut-over.
  Where the bash tool and its suites were deleted, the oracle was replaced, not dropped: the
  freeze tools' white-box bash suites are replaced by a golden replay of the shipped bash tools'
  recorded output (stdout, stderr, status and commands over a scenario corpus) plus Go tests, and
  qa-all's by a golden replay of the legacy script plus the Go tests that took over the cases of
  the deleted `qa-helper-summary` suite. That meets the criterion because the replay compares the
  Go with the bash byte for byte, and a difference is allowed only as a numbered bug pinned with
  its reason, which a suite of a deleted tool could no longer do.
- [ ] Each defect class found in the ports has a rule, or a recorded reason it cannot
  ([GoProjectRules.md](../../GoProjectRules.md): `project_waitdelay` for the dropped capture
  (B25); the `sudo -n -v` probe is recorded as not a rule, with what holds it).

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
