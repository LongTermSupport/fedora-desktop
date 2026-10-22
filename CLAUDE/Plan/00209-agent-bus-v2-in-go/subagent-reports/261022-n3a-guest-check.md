# n3a guest-check (Plan 00209 N3, slice n3a, step 08 part a)

Branch `feat/bus-v2-n3a-guest-check`, from `17bdc45f1`. Commits:

- `6eba1bc87`: the shared types (`identity.go`, `output.go`) and exits' 78 codes. This is the
  early commit the place-bundle/desired/remove/set-aside builder writes against.
- `a2d4f9628`: `guest check` (GU2), the cli wiring, the qa rules row and the suites row.

## What was built

`internal/teamchat/member/guest`:

- **`identity.go` (GU1)**: `System` is a seam over the process ids, the umask and passwd.
  `OS{}` is the real one. `Account` is a passwd entry.
  - `Establish(sys, paths.GuestUser) (*Identity, error)` sets umask 077 first. It then refuses
    each of these with 78 `identity`:
    - uid 0;
    - a set-id caller (euid differs from uid, or egid from gid);
    - no passwd entry;
    - a passwd uid that differs from the caller's;
    - a caller uid whose passwd name differs from `--user`;
    - a gid that is not the account's;
    - a supplementary group other than the account's gid.
  - It opens the passwd home with `homefs.Open(home, homefs.Owned)`. A failure there is
    78 `home`.
  - No message quotes a passwd value.
  - `Identity{User, UID, GID, Home *homefs.Root}`, with `Close()`.
- **`output.go` (the output contract)**:
  - `What` is a changed item. `Op{Verb, Changes []What, Busy bool}` is an op's contract, and
    `register(op)` records it (a verb registered twice panics).
  - `Ops()` and `ChangesOf(verb)` return copies of the sets for n3b's TB2.4 parser.
  - `NewOutput(w, op).Changed(what)` prints `CHANGED\t<what>\n`. A what outside the op's set
    panics, because that is a code mistake the host would refuse.
  - `Finish(stderr, op, err) term.ExitCode` writes through `exits.Write`. It lets 64 and 78
    through, and 75 only when `op.Busy`. Any other exit becomes 1 `internal`, which is a bug.
- **`check.go` (GU2)**:
  - `OpCheck` has no changes.
  - `Checker{Sys, Run execx.Runner, Kernel, SystemDirs}`, with `NewChecker()` the real one.
    `UnitPath(home, SystemDirs)` gives `<home>/.local/bin:/usr/local/bin:/usr/bin:/bin`.
  - `Check(ctx, user, team, stdin)` runs in this order, stopping at the first miss:
    1. Establish.
    2. The strict stdin: exactly the 4 keys, v 1, team equal to `--team`, capped at 16 KiB.
       A miss is 64 `usage`, quoting nothing.
    3. `gh`, `tmux` and `claude`, each the first executable regular file on the unit PATH. A
       miss is 78 `gh`, `tmux` or `claude`.
    4. `gh auth status --hostname github.com`, through execx at the found path, with env
       `PATH=<unit PATH>` and `HOME=<home>`. The output is captured and never printed, and the
       call is bounded at 30 s. A miss is 78 `gh`.
    5. `Kernel.Pidfd()`, then `Kernel.Inotify(home)`. A miss is 78 `kernel`.
    6. Reach: `matrix.NewMember` with the placeholder token `unused`, then `Versions` within
       3 s. Either failing is 78 `reach`.
  - `tmux` is only stat'ed.
  - `LinuxKernel` uses the real `pidfd_open` and `inotify_init1` + `inotify_add_watch`.
- **Tests**: execx.Fake for gh, a fake kernel, and a fake system over real temporary
  directories. Reach is tested on fakehs: it passes, it fails on a URL its hosts do not allow,
  and it fails with the server down. One test checks that the only request was a versions
  request.

Other changes:

- `member/exits`: the 78 row gains `identity home gh tmux claude kernel reach`, and its
  meaning text gains "a guest's identity or check".
- `member/cli`: `"guest check": guestCheck` moved into `runs` and out of `unbuilt`.
  - `cli/guest.go` holds `guestCheck`. Its tests are in `cli/guest_test.go`: through `Run`, the
    test's own caller is refused with 78 `identity`, and an invocation missing its flags is 64.
  - `run_test.go` and `verbs_test.go` drop the `guest check` row.
  - gofumpt realigned the two existing lines of the `runs` literal, so the other builder's
    wiring will conflict there. The conflict is trivial.
- `qa/rules/table.go`: a `dynamicNames` row for `Checker.ghLoggedIn`, because gh runs at a
  path found at run time.
- `scripts/go-binary-suites.tsv`: added `teamchat	qa-go`. The cli part added `cmd/teamchat`
  without this row, and `qa/changed`'s real-tree test fails without it. That test runs on any
  `qa/` change.

## Coverage

guest 96.4%, exits 98.9%, cli 99.7% (`go test -race -cover`). `golangci-lint run` reports 0
issues with and without `--build-tags tuwunel`. `qa-changed.bash` is green, govulncheck
included.

## Decisions, with proposed design text

1. **78 gains the guest codes.** Proposed B 2.2.4 78 row: codes `bundle, membership, store, forge-login, runtime, launch, identity, home, gh, tmux, claude, kernel, reach`; meaning
   "configuration refused (bundle, desired membership, store, launch, a guest's identity or
   check)".
   - B 2.13 sentence: "A caller that is not the user named is 78 `identity`. A home that is
     not the user's own is 78 `home`. Each guest check miss is 78 with the check's name."
2. **Set-id callers and the primary gid are refused too.** Besides SPEC GU1's list, euid must
   equal uid, egid must equal gid, and gid must be the passwd gid. `getgroups` may list the
   primary gid. Proposed GU1 text: "refuses uid 0, a set-id caller, a gid or supplementary
   group but the user's own ...".
3. **A stdin miss is 64 `usage`.** This covers a bad shape, a team other than `--team`, an
   oversized stdin (over 16 KiB) and an unreadable one. A bad `base_url` for its
   `plain_http_hosts` is 78 `reach`. Proposed n3-D6 sentence: "a stdin that is not exactly this
   object, or names another team, is 64 `usage`".
4. **The program lookup** follows links, as exec does: `~/.local/bin/claude` is a link from
   Claude Code's installer. It passes over a match the caller cannot execute (`access(X_OK)`),
   so the first executable one on the unit PATH is the one gh runs as. Proposed GU2 wording:
   "the first executable regular file (links followed) of each name on exactly the unit's
   PATH".
5. **gh runs at the found path, with env `PATH=<unit PATH>` and `HOME=<home>`**, bounded at
   30 s (the design gives no bound). Its output is captured and never shown, because it names
   the account.
6. **inotify "usable"** means an instance plus a watch on the home (`IN_CREATE|IN_ONLYDIR`), so
   `max_user_watches` 0 is caught. pidfd means `pidfd_open(getpid())`.
7. **Op registry**: each op's file declares `var OpX = register(Op{...})`, so the two builders
   never edit one list. `Finish` makes an exit outside an op's contract into 1 `internal`.

## Notes for the next parts

- **The parallel builder (GU3-GU5, SA1)** reuses these:
  - `Establish(guest.OS{}, user)`, then work only through `id.Home` (homefs).
  - Declare `var OpPlaceBundle = register(Op{Verb: "guest place-bundle", Changes: []What{...}, Busy: true})`
    in its own file, and the same for `guest membership` (not Busy), `guest remove` (Busy) and
    `seat set-aside` (Busy). `TestOpsAreRegistered` accepts the `seat set-aside` verb.
  - Print with `NewOutput(e.Stdout, OpX).Changed(...)`, end with `Finish(e.Stderr, OpX, err)`,
    and refuse with `exits.New(code, &team, template, fills...)`. 75 is `exits.CodeSeatHeld`.
  - Wire each verb as `cli/guest.go`'s `guestCheck` does: one `runs` line, and one name dropped
    from the `"n3a guest"` row of `unbuilt` and from `verbs_test.go`'s want list.
  - Note that Establish sets the real process umask. A cli test restores it, as
    `TestGuestCheckIsBuilt` does.
- **n3b**:
  - TB2.4's parser takes `guest.ChangesOf(verb)` and `exits.Decode` for the last stderr line.
  - The session unit's `PATH` must be `guest.UnitPath(home, guest.SystemDirs)`. If n3b wants
    it in `paths` as the one source of names, move it there and register it in paths' tests.
- `TestGuestCheckIsBuilt` assumes the test's own caller is root or is not named `agent`.
