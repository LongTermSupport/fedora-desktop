# n2g part A handoff 2: A4 mostly built (Plan 00209, SPEC-n2g.md)

Worktree `/workspace/untracked/worktrees/bus-v2-n2g`, branch `feat/bus-v2-n2g`. Read first: the
earlier handoff `261022-n2g-a-handoff.md` (A1-A3, its decisions) and
`/workspace/untracked/scratch/n2g-part-common.txt`. Every commit below had qa-changed green and
golangci clean with and without `--build-tags tuwunel`.

## Done in A4

| Commit      | What                                                                                       |
| ----------- | ------------------------------------------------------------------------------------------ |
| `dfd14e6f1` | RC7: `checkStarted` takes `[own gid]` as the one group; `Process.Getgid()`                 |
| `d780332dd` | ctl: `restoreFiles` (Swap, Finish, Revert, WriteLost); `Config.Sleep`; `Opened.Staged`     |
| `ead39222c` | `restore/child.go` (Child + arg types); Answer `Export`/`Epoch`, codec, `answerFits` (RC6) |
| `5d3b063fe` | `admin/serverestore.go`: the child serves the six restore ops (RC1-RC5, RC8)               |

The lead was sent the Opened fields and the restore.Child types by message.

### The API as built

- `admin.Opened` gains `Restore RestoreFiles` and
  `Staged func(ctx, port int, token string) (*matrix.Admin, error)`; `admin.StagedWait` (60 s).
- `admin.RestoreFiles`: `Swap(token, rooms, roster []byte) error`, `Finish() ([]string, error)`,
  `Revert() ([]string, error)`, `WriteLost(n int64, data []byte) error`.
- ctl (`ctl/restore.go`): each op reopens `ctl/<team>/` by `openDir`'s rule and sweeps its
  names' temporaries. Swap refuses when any of the three files is missing, before any change;
  it links `<name>` to `<name>.previous` (only when that is absent, then a directory fsync) and
  replaces `<name>` through a temporary and `renameat`. Revert is `renameat(.previous, name)`.
  Staged probes `versions` every 1 s through `Config.Sleep`, up to `admin.StagedWait`.
- `admin.Process.Getgid()`.
- `restore.Child`, `SecureArgs`, `CheckArgs` (with `Live()`), `SwapArgs`, `ExportArgs`, `Export`
  and `Epoch` in `restore/child.go`, exactly the shape in the first handoff. **Merge glue:** C
  (`feat/bus-v2-n2g-restore`) was asked by message where its copy lives (no reply had come);
  one copy must be dropped at merge.
- Ops: `admin.OpRestoreSecure|Check|Swap|Export|Finish|Revert` (`restore.secure` ...). Their wire
  args: `restoreArgKeys` in `serverestore.go`. `rooms` is the rooms.json object, `roster` the
  roster-file object, `port` and `token` for check are both null for the live server.
- Answer: `Export *restore.Export` (key `export`: count, first_ms, end_ms, truncated) and
  `Epoch *restore.Epoch` (key `epoch`: restore_epoch, rev). `answerFits` ties each to its op,
  and refuses a handle, credential, main room or notes on any `restore.*` op.

### Decisions to record in `261022-n2g-a.md`

- RC1 checks the staged event's rev (refused, 70) before any change, not only before the
  write. A retired handle is kicked only where joined or invited, and deactivated only when found
  and active, so a repeat changes nothing. The random password is `crypto/rand.Text()`.
- RC8 reads the main room from the child's `rooms.json`, since the spec gives check no rooms
  arg. A backup whose main room differs from today's fails the verify (fails closed). Proposed
  design fix: give `restore.check` a `rooms` arg. It also refuses a roster event whose sender is
  not the team's admin.
- RC3: the export's `end_ms` is `max(now, newest kept event)`. Each room is read backwards in
  pages of 100 until an event at or before `since_ms`; a room that alone passes the cap is cut.
  The merge across rooms then keeps the newest under 64 MiB.
- RC2 refuses a swap when a file is missing (revert could not restore an absent file).
- The restore ops' file failures answer `failed` (1), and a missing seam answers `config` (78).

## Left

1. **Root side, `admin/restorechild.go` (test first).** It is `RestoreChild{Live Child; Unit func(argv []string) Child}`, which implements `restore.Child`. Live is the ExecChild; Unit
   starts the child through a unit's argv. Each method encodes the args (rooms as
   `apply.EncodeRooms`, roster as `f.Encode()`, both parsed to proto values; the team file is
   parsed from its bytes). It asks, applies `answerFits(op, a)`, maps `!a.OK` to
   `&ChildError{Class, Message}`, and answers `a.Changed`, `*a.Epoch` or `*a.Export`. `Check`
   with a nil unit and `Live()` args sends null port and token through Live. Add a `UnitChild{Runner execx.Runner; Argv []string}` whose `Ask` runs the argv as root, with CleanEnv, `PATH` only,
   the request on stdin and stdout capped. It judges with `judge(op, ...)` as `ExecChild.Ask`
   does; refactor the tail of `ExecChild.Ask` into a shared helper. Test with `inProcessChild`
   (world_test.go) as Live and as Unit, and with `execx.Fake` for UnitChild.
2. Then SendMessage the lead (`main`) with RestoreChild's exact shape. Also tell them that C's
   two host asks (`ReadUnitsVersion(root, team)`, an exported `RegKeyOf`) are not built, unless
   the new agent builds them in `host`.
3. A5: nothing.
4. The final report, `subagent-reports/261022-n2g-a.md`. It covers A1 onwards: the API as built
   (this file and the first handoff), coverage, decisions (both files) and the merge glue. Last
   measured coverage: admin 95.7%, admin/wire 92.0%, ctl 92.9%.
