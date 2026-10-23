# n3b P1 handoff (Plan 00209 N3, slice n3b, part P1)

The builder stopped at its context budget. This file is the state, so a fresh builder can
finish P1. The brief is section 3 of `261022-n3b-handoff.md`, with sections 0, 2a and 7. The
common rules are `/workspace/untracked/scratch/n3b-part-common.txt` (part = p1).

Worktree `/workspace/untracked/worktrees/bus-v2-n3b-p1`, branch `feat/bus-v2-n3b-p1`.

## Done

1. **Gap 15, the QA rule change.** Committed `aec0dc464`, with qa-changed green.

   - `lxccmdPkg` is a second owner of `lxc-ls`, `lxc-info` and `lxc-attach` in
     `qa/rules/table.go`.
   - There is a `dynamicNames` row for `LXC.Guest` with the brief's reason.
   - `walkProgram` now takes the whole `excuse`. An inner wrapper with a dynamic row and a
     non-literal program after `--` is excused. A wrapper that is not inner is not excused.
   - The fixture is `qa/rules/testdata/src/fix/internal/teamchat/host/lxc/lxccmd/lxccmd.go`
     and the test is `TestSeamCommandsInnerDynamic`. Red was seen before the change and green
     after it.
   - The package doc of `project_seam_commands` and `CLAUDE/GoProjectRules.md` each gained the
     sentence the brief asks for.

2. **guestroot**, at `src-go/internal/teamchat/host/lxc/guestroot/`. Its work in progress is
   committed with this file. It is built to the brief's API.

   - **Tests:** all pass under `-race`, at 96.3% coverage. They take about 200 s, almost all
     of it in `TestEveryFailingCallFailsItsOperationAndLeaksNothing`.
   - **Source lint:** `sys.go`, `root.go`, `read.go` and `write.go` are clean under
     golangci-lint.
   - **Test lint:** 35 findings remain in the four test files, listed below.

## Left

1. **Make the guestroot test files lint-clean.** Run
   `golangci-lint run ./internal/teamchat/host/lxc/guestroot/` from `src-go`. The findings:

   - **gosec G301, G302, G306:** the fixtures need modes such as 0755, 0775 and 0757, because
     those modes are what the code must check or refuse. Add one exclusion hunk to
     `src-go/.golangci.yml`, `path: internal/teamchat/host/lxc/guestroot/.*_test\.go`,
     `text: "G301|G302|G306"`, with that reason. Keep it as its own hunk, beside the
     playlock one.
   - **gosec G115, in the recorder:** `uint64(int)` on the openat, flock and unlinkat flags.
     Give `call` a second field, `oflags int`, for the int-flag calls. `TestReadFile` and
     `TestProbeLock` then compare `oflags`.
   - **gocognit and cyclop:** split `TestTB1RefusesEveryNonRegularRead`, `TestReadFile`,
     `TestDialSocket`, `TestProbeLock`, `TestWriteFile`, `TestPlaceDir`, `TestSymlink`,
     `TestPinHoldsALiveInit`, the fault sweep and `startFIFOWriter` into helpers or subtests.
   - **govet shadow:** rename the inner `err`s.
   - **dogsled:** in `TestReadsOnADeadRootAreGone`, use a helper per call.
   - **modernize:** use `w.done.Go(...)`.
   - **prealloc:** two loops in the tests.
   - **Unrelated to lint:** `startFIFOWriter` must finish one round of polls before it looks
     at `stop`. Today a fast test can stop it with 0 polls, which is flaky. It passed on the
     last run.
   - **Optional speed-up:** the fault sweep could share one fixture per scenario and
     re-create only what each run consumes.

2. **lxccmd**, at `src-go/internal/teamchat/host/lxc/lxccmd/`, from section 3 of the brief.
   Nothing is written yet. Points worked out so far:

   - **The Guest argv.** Build it as one literal `append([]string{"-n", c, "-u", uid, "-g", gid, "--clear-env", "--set-var", ..., "--", a.Kit.Binary()}, words...)`, inside `LXC.Guest`
     itself. The rule then sees `--`, and the non-literal program after it is excused by the
     row. The row is matched by `pkg` suffix and function `LXC.Guest`, so the method must
     have exactly that name, on a type named `LXC`. Words is `strings.Fields(op.Verb)`, then
     `--user u`, then args.
   - **Container names.** P3 adds `paths.ContainerName`, but P1 cannot import it, since P3 is
     built in parallel. `declare`'s container regexp is unexported:
     `^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$` (`declare/values.go:26`). Keep a copy in lxccmd and
     note it in the report, so P4 can switch to `paths.ParseContainerName`.
   - **Exits and decoding.** `exits.Decode(line)` gives an `exits.Error` with `Exit` and `Code`.
     `guest.ChangesOf(verb)` is the closed set of what an op may report. `guest.OpCheck` must
     print nothing.
   - **AttachError (n3b-D1).** It is new: `lxc-attach` itself failed with no error object. Say
     an exit of 1 or other with an empty last stderr line, or a start failure. P4 then asks
     `State` again.
   - **InitPID.** It must be `> 1` and `<= pid_max`. Pass pid_max in, or read it through a
     seam. The host's is 4194304.

3. **The report** `subagent-reports/261022-n3b-p1.md`, as the common rules ask: what was built,
   the exported API exactly, coverage, and decisions. Give a final message under 600
   characters.

## API changes and additions against the brief, with reasons

- **`ErrGone` is new**, as section 2a asks. Every Root call, the Writer's included, runs
  between two `Alive` checks, so a dead init answers `ErrGone`.
- **`Root.InitExe() (Info, error)` is new.** It opens the host's `<Proc>/<pid>/exe` with
  `O_PATH`, following the link (n3-D17), so P4 can compare its device and inode with the
  guest's `/usr/lib/systemd/systemd` from `Stat`. LX4's mechanics are P1's.
- **`LinuxKernel.PidfdAlive` polls the pidfd** for `POLLIN` (readable means exited), rather
  than calling `pidfd_send_signal(0)`. The signal call succeeds on a zombie, and poll does
  not.
- **`ReadProcStat(pid) (data, found, err)`** is the brief's `ReadProcStat`. The brief's
  comment line called it `ReadProc`.
- **`ProbeLock` on a missing lock** returns an error wrapping `fs.ErrNotExist`. P4 reads that
  as free, with no record.
- **`Writer.WriteFile` replaces anything at the name** that is not a matching regular file: a
  link or a FIFO is replaced unopened, and a directory fails the rename.
- **`Symlink` replaces a file or a link**, and fails on a directory.
- **A caller may not pass a name starting `.new-`**, which is the temporary prefix.
- **`RemoveTree` is bounded** at 16 levels deep and 4096 entries per directory.
- **`OpenDir` roots have no kernel.** `Alive` is always nil for them, and `InitExe` errors.
- **The bind-mount test skips** where mounting is not permitted, as in this container.
  `TestMkdirAll` proves through the recording seam that `RESOLVE_NO_XDEV` reaches every write
  open.
