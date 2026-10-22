# n3a guest-check (Plan 00209 N3, slice n3a, step 08 part a)

Branch `feat/bus-v2-n3a-guest-check`, from `17bdc45f1`. Commits:

- `6eba1bc87`: the shared types (`identity.go`, `output.go`) and exits' 78 codes. This is the
  early commit the place-bundle/desired/remove/set-aside builder writes against.
- `a2d4f9628`: `guest check` (GU2), the cli wiring, the qa rules row and the suites row.
- `6106edbc0`: `feat/bus-v2-n3a-guest-place` (tip `00261a332`: GU3-GU5, SA1, its report
  `261022-n3a-guest-place.md`) merged with `--no-ff`, with the glue its report lists (see
  "The merge" below). This branch now holds all of step 08.

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

## The merge (both parts)

These are the guest-place report's six glue items, in the same order:

1. **Identity**: `cli.identify` is a seam whose default is `identifyCaller`. It reads
   `--user` and calls `guest.Establish(guest.OS{}, u)`, which sets umask 077 first and runs
   GU1's refusals. It returns `*guest.Identity`.
   - `guest.Home` is gone. The ops take `*Identity`, whose home field is renamed `Home` to
     `Root`, and `layout()`/`config()` are now methods on it.
   - `guest check` keeps its own `Establish` inside `Checker.Check`, so its tests can drive a
     fake `System`.
2. **Output**: the ops' `changes` interface is now `Changed(what What) error`, which
   `*guest.Output` satisfies. The `What*` constants are typed `What`. `changedLines` is gone.
3. **Contracts**: one registry. places.go registers `OpPlaceBundle`, `OpRemove` and
   `OpSetAside` (all Busy) and `OpMembership` (not Busy). Every cli verb ends through
   `guest.Finish(stderr, op, err)`, and `asGuest` hands each op `guest.NewOutput(stdout, op)`.
   - The four `[]string` sets are deleted. Their test now checks each what against
     `ChangesOf` and checks Busy.
4. **Package comment**: identity.go's is kept and places.go's is dropped.
5. **unbuilt**: the `"n3a guest"` row is gone, with its entries in `run_test.go` and
   `verbs_test.go`. `guest check` has a verb constant, `verbGuestCheck`, beside the others.
   - Under `-tags teamchatrelease` 19 verbs remain unbuilt, none of them n3a's.
6. **Claim id**: `seat.newClaim` is exported as `seat.NewClaim` and its callers in seat are
   renamed. place-bundle uses it, so its copy is deleted.

A note on the error objects: Finish turns an exit outside an op's contract into 1 `internal`.
So guest-place's plain errors (an unreadable stdin, an unset `XDG_RUNTIME_DIR`) stay exit 1
`internal`, as its report already says.

## Coverage

These are after the merge (`go test -race -cover ./internal/teamchat/...`, all passing):

| package                       | coverage |
| ----------------------------- | -------- |
| guest                         | 93.9%    |
| cli                           | 99.1%    |
| exits                         | 98.9%    |
| seat                          | 91.0%    |
| paths (guest-place's, merged) | 100%     |

`golangci-lint run` reports 0 issues with and without `--build-tags tuwunel`.
`qa-changed.bash` is green: docs, go-qa on both modules, and teamchat-keeps-off-v1.

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

- **A new guest op** takes `*guest.Identity`, registers `var OpX = register(Op{...})` in its
  own file, prints through `*Output`, and is wired in cli through `asGuest(e, inv, OpX, ...)`.
  - Establish sets the real process umask. A cli test that reaches it restores the umask, as
    `TestGuestCheckIsBuilt` and `TestDefaultGuestSeams` do.
- **For the lead's review**: `paths.Layout.SeatClaim`'s comment still says "swapped in"
  (guest-place's note).
- **n3b**:
  - TB2.4's parser takes `guest.ChangesOf(verb)` and `exits.Decode` for the last stderr line.
  - The session unit's `PATH` must be `guest.UnitPath(home, guest.SystemDirs)`. If n3b wants
    it in `paths` as the one source of names, move it there and register it in paths' tests.
- `TestGuestCheckIsBuilt` assumes the test's own caller is root or is not named `agent`.
