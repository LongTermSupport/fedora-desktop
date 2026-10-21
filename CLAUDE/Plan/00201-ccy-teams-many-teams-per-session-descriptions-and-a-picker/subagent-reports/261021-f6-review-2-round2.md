Verdict: FIX-FIRST

# F6 review 2, round 2: fix/membership-strict-json (2282ced4, 12568828, 161339b3 on f460abb7)

There is one blocking finding. The two readers now agree on every probe I could build: 106
probe files, plus 3 more, with the only differences being the documented int64 departure and a
Python crash. Every fix from round 1 is real, and a test catches each one if it is reverted. But
the launcher's writer, `membership-init`, still writes generation 1 without `check()`. So the
one Go writer that writes every session's first file can write a file that both readers refuse,
or one that names a different checkout.

## What holds (with evidence)

- **Tests and lint pass.** `go test -race -count=1` passes over `./internal/teams/...` and
  `./cmd/ccy-teams/...`. `golangci-lint run` over `teams/membership/...` and `teams/hot/...`
  reports 0 issues. `go vet` is clean.

- **Go and pingbus agree on every probe except the documented int64 departure and a Python
  crash.** I ran 106 probe files through both readers: 67 new ones (`f6probe2/gen.py`) and the
  39 from round 1 (`f6probe/gen.py`). The Go side ran through an overlay test
  (`f6probe2/probe_test.go`, `go test -overlay`). The Python side was `read_membership.py`, run
  once per file so that a crash stays isolated. Only two differ:

  - `r_big_gen`: the documented int64 departure.
  - `r_deep`: Python crashes with `RecursionError`, as in round 1.

  The new probes cover these areas:

  - **Surrogates.** A pair written as two escapes ×4095 (ok) and ×4096 (refused). Reversed
    pairs, which pyjson joins just as CPython does. A lone high surrogate at the end. Lone
    *low* surrogates ×4095 and ×4096. A high escape before `A`, at both the bound and one
    over it. high-high-low. A high escape before a bad `\u` escape, and before `\n`. Lone
    surrogates mixed with raw astral characters. Raw `ED A0 80` bytes, which strict UTF-8
    refuses.
  - **Controls.** DEL, NUL, US, C1, a trailing `\n`, and a bare `/`.
  - **Seat and team grammar.** A comma or `@` in a team or seat, an empty seat, and a trailing
    space.
  - **Keys and types.** A miscased key plus a missing one, an unknown key plus a missing one, a
    list as `kind`, `true` as `v` or `generation`, 64 and 65 seats, a lone-surrogate key, a
    duplicate nested key, an object or `null` where a list is expected, a negative or zero
    number, session names of 255 and 256 characters, a session ending in a newline, a BOM,
    `1E0`, `01`, `-1`, and a trailing comma.
  - **Reports.** A comma in a team, 64 and 65 told roles, `orchestrator`, `told` or `inbound`
    null, `inbound: 1`, a miscased `Told`, holders of 33 or 0 characters, an unknown state, `-`
    as the handle separator, a handle repo of 48 and 49 characters, and 65 seats.

- **Round 1 finding 1 (comma in a team or seat) is fixed.** `checkEntries` judges each entry
  directly (`desired.go:131-143`). I put the `ParseSeats(Canonical(...))` round trip back as a
  mutant. Two tests catch it: the table rows `desired: a team with a comma` and `a seat with a comma` in `TestReadersTakeExactlyWhatTheWritersWrite`, and
  `TestWriteRefusesAMembershipPingbusWouldRefuse`.

- **Round 1 finding 2 (lone surrogates counted in bytes) is fixed.** `characters()`
  (`desired.go:44-55`) counts a WTF-8 surrogate as one character. The rule is right for both
  halves of the range (`ED A0-BF`): the probes with lone low surrogates agree at 4095 and 4096.
  If `characters` is reverted to `utf8.RuneCountInString`, the table row `a checkout of 4095 lone surrogates` fails. The truncated-sequence branch (`i+2 < len(s)`) is reachable only by
  `Write` with invalid UTF-8, and `Write` then refuses at the `ValidString` check anyway.

- **The nits were addressed, and each is tested.** I ran mutants through the overlay to check
  that a test catches each one:

  - Removing `Write`'s UTF-8 check: caught (`strict_test.go:381`).
  - Naming an unknown key before a missing one: caught
    (`TestAMissingKeyIsNamedBeforeAnUnknownOne`).
  - Dropping the miscased-key message: caught
    (`TestAnUnknownKeyAndAMiscasedKeyAreToldApart`).
  - Dropping the 64/256 bound in `checkEntries`: caught (`strict_test.go:394`).
  - Dropping the seat/team grammar: caught.

  The doc comment fix ("every required one present"), `integer` reusing `integer64`, and the
  pins for `HANDLE_SEP` and `HANDLE_TYPES` are all in.

- **The key-order logic in `object()` cannot change a verdict.** It accepts exactly when there
  is no missing key and no unknown key, which is also when Python's `_object` accepts. Only the
  message differs, and only for a miscased key. `slices.DeleteFunc(given, …)` mutates `given`,
  but only after `missing` has already been computed from it, so it is safe.

- **The callers still behave correctly.**

  - `discover.go:240`, `restart/jobs.go:212`, `hot/hot.go:462` and `hot/list.go:211/295` call
    `Read`.
  - `hot/add.go:387/430/461`, `hot/role.go:255`, `hot/cc.go:139` and `hot/list.go:176` call
    `ReadReport`, `ToldRole` and `Inbound`.
  - `hot.go:593` calls `Write`.
  - The typed `Told` has a nil-safe `ToldRole`. The hot test rig now marshals a real `Told` and
    reads it back.

- **The commit 161339b3 correction of the QA paragraph is accurate.** It matches round 1's
  measurement: the coverage counts membership and hot only.

## Blocking findings

1. **`membership.go:159` (`Init.Run` → `writeDesired`), with `desired()` at
   `membership.go:181-205`. The launcher writes generation 1 without `Desired.check()`.**

   `desired()` checks the checkout only with `filepath.IsAbs` and `filepath.Clean`, and the
   seats only with `ParseSeats`. It then calls `writeDesired` directly and never reaches
   `Write`. I probed three inputs (`f6probe2/probe_test.go`, `TestProbeInit`):

   | Input                                   | What happens                                                                                                                       |
   | --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
   | `$PWD` = `/tmp/a<TAB>b`                 | init writes the file, then `Read` refuses it: "not an absolute path free of control characters". pingbus refuses it too (exit 78). |
   | 65 seats in the `--teams` list          | init writes the file, then `Read` refuses it: "seats is not a list of at most 64". pingbus refuses it too.                         |
   | `$PWD` = `/tmp/a\xffb` (a Latin-1 name) | init writes `"/tmp/a�b"`. Both readers accept it, and both name a different checkout. `Write` refuses the very same `Desired`.     |

   Why this blocks:

   - **It is the first writer of every session.** This is the file the session's pingbus reads
     first.

   - **The launcher expects a refusal at launch.** `agent-bus-membership.bash:62-64` and
     `cc-teams.bash:137` both rely on membership-init refusing bad input, so that the launch
     stops with the reason. `membership_test.go:196-197` says the grammar copy is held "so the
     launcher's list is checked before anything is written".

   - **Instead the session fails later.** It starts, and its pingbus fails at runtime with exit
     78\. In the non-UTF-8 case, nothing fails at all: the session runs against the wrong
     checkout path. This breaks the fail-fast rule.

   - **The commits overclaim.**

     - 12568828 says "Desired.check holds the last two, so Write refuses them too" and "the Go
       writers' output still reads back".
     - 161339b3 says "Write refuses a checkout that is not UTF-8 (encoding/json would write it
       as U+FFFD, naming another checkout)".

     Each is true of `Write` and false of the launcher's writer, which is the one that writes
     generation 1.

   - **It predates the branch, but the branch is where it became contract.** Before this
     branch, `check()` held no more than `desired()` did. This branch made `check()` the
     shared grammar and left the launcher outside it.

   Fix:

   - Give the launcher the same gate as `Write`. Two ways:
     - make `desired()` end with `d := Desired{...}; return d, d.validForWrite()`, where
       `validForWrite` is `check()` plus the `utf8.ValidString` test that `Write` has now; or
     - move the UTF-8 test into `isCheckout` (`check()` would then refuse a Read of a
       lone-surrogate checkout, which pingbus takes), so better keep it as a write-side check
       that `Write` and `desired()` both call.
   - Either way, `Run` turns the error into `ExitUsage` with the reason, before anything is
     written.
   - Add these rows to `TestInitRefusesWhatIsNotItsThreeArguments`:
     - `"a control in the checkout": {"s", "/c\tx", ""}`
     - `"a checkout not UTF-8": {"s", "/c\xff", ""}`
     - `"65 seats": {"s", "/c", <65 items>}`
   - The existing assertions ("wrote before refusing") then pin the behaviour.

## Nits

- **The low-surrogate half of `characters()` is not pinned.** I narrowed the range to
  `0xA0-0xAF` (high surrogates only) as a mutant, and it survived every test, because the table
  only uses `\ud800`. Add a row of 4095 and of 4096 `\udfff`. My probe shows both readers
  already agree on those.

- **A file `Read` takes but `Write` will not rewrite.** A desired file whose checkout holds a
  lone surrogate is read by both readers, but `Write` refuses the `Desired` that `Read` returns
  (WTF-8 is not valid UTF-8). The result is that `ccy-teams` hot add/remove on such a session
  fails at `hot.go:593` with "not UTF-8". That is fail-fast and correct, because rewriting it
  would change the checkout. No writer produces such a file. A sentence in the `Write` comment
  saying so would stop someone "fixing" it later.

- **`hot/list.go:176-178` still drops `repErr` silently.** This was raised in round 1 and is
  not addressed. It predates the branch, but the stricter reader widens what is dropped. Put
  the error in `row.State`, as line 171 does for the desired file.

- **The `HANDLE_SEP` pin is weak.** `strings.Contains(handleName.String(), `+`)` would still
  pass if the `\+` moved elsewhere in the pattern. Building the expected separator span
  (`seatPattern + `)+` + handleHostPattern`) and checking for it would be exact.

- **Two Python-side defects, out of scope for this branch.** pingbus should get an issue for
  each:

  - `parse_desired` crashes with `TypeError: unhashable type` on a farewell `reason` that is a
    list or an object (`entry["reason"] not in FAREWELL_REASONS`, which is a dict). It should
    raise `MembershipError`.
  - The `RecursionError` on deep nesting, as in round 1.

  In both cases Go refuses, which is the right verdict. The parity table does not hold these
  cases, and correctly so, because the helper would crash.

- **Not verified.** I did not check the "27 of those cases were judged wrongly" claim in
  12568828\.
