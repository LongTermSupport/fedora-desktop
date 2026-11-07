# PR #128 review: B3, S2, S3 (builder report)

Branch `fix/crossnet-b3-s2-s3`, worktree `.claude/worktrees/crossnet-b3`, based on the local head
of `spike/v2-cross-network` (8994d156d). Four commits, local only: nothing pushed, nothing merged.

| Commit    | What                                                                                          |
| --------- | --------------------------------------------------------------------------------------------- |
| 9590c875e | B3, Defence Before Fix: the class detector and the X29 deny assertions, committed red         |
| df4078a03 | B3 fix: both teamchat drop-ins render `IPAddressDeny=any` and `SocketBindDeny=any`            |
| be448f45d | S2: the default team url is https; non-loopback http refused at `declare check` and `urlHost` |
| c845aecaa | S3: a url whose effective port is not `team.Port` refused at both                             |

## B3: does sandbox.go's deny reach the terminator? No

`host/sandbox.go`'s `baseSandbox` is the base unit text of the **homeserver**,
`teamchat-server@.service`. systemd merges a drop-in only with its own unit's base text
(`teamchat-tls@.service` and the `.d/` directories that belong to that unit), never with
another template's. So that deny never applies to `teamchat-tls@<team>`. SPEC X18a says the
terminator's base unit should carry `IPAddressDeny=any` and `SocketBindDeny=any`, but no Go code
and no play makes that unit's text: `tlsBaseInstalled` only checks that the file exists. The
homeserver's own drop-in had the same flaw: an allow-list alone, relying on a base unit that a
play not yet built installs, and that apply reads without checking it. So both drop-ins now carry
their own deny. They share one helper pair in `host/render.go`: `ipFilterLines` and
`socketBindLines`, each emitting the deny before its allow.

## Defence Before Fix

- **Class:** a systemd IP filter or socket bind written as an allow-list with no deny of `any`.
  **Hazard:** systemd grants whatever no rule denies, so `IPAddressAllow=` or `SocketBindAllow=`
  alone restricts nothing. The tests checked only the allow line, so they passed on a unit that
  let every address in.

- **Detector:** a test over the rendered text, not a `qa/rules` analyzer. An analyzer sees
  string literals, not the unit text a render puts together, so it cannot judge this class.
  `v1names/units_test.go`:

  - `TestEveryUnitTextDeniesWhatItDoesNotAllow` judges `filteredUnits`, which is `madeUnits` plus
    the homeserver's and the terminator's drop-ins, read through the new
    `host.DropIns`. Wherever a unit text has `IPAddressAllow=`, it must also have
    `IPAddressDeny=any`; wherever it has `SocketBindAllow=`, it must also have
    `SocketBindDeny=any`. Each drop-in is judged on its own text.
  - `TestTheFilterCheckDiscriminates` proves the check can fail.
  - `TestEveryUnitTextSourceIsJudged` (already there) makes any new source of unit text join the
    table. The rows for `host/render.go` and `host/tlsunit.go` now name `filteredUnits`.
  - `host.DropIns` is checked equal to render's output by `TestDropInsAreRendersDropIns`.

- **Red, before the fix:** "the homeserver's drop-in has ["IPAddressAllow=" "SocketBindAllow="]
  with no deny of any, so it restricts nothing". The same message came for the terminator's
  drop-in.

- **Sweep:** every unit-text source in the teamchat tree.

  | Unit text                 | State before                       | Action           |
  | ------------------------- | ---------------------------------- | ---------------- |
  | `host/tlsunit.go` drop-in | allow only                         | fixed (B3)       |
  | `host/render.go` drop-in  | allow only                         | fixed            |
  | `restore/argv.go` staging | `TeamSandbox` denies               | holds            |
  | `restore/argv.go` secure  | `IPAddressDeny=any`, no bind allow | holds            |
  | `backup/units.go`         | deny and allow                     | holds            |
  | `host/lxc/unit/unit.go`   | no filter                          | not in the class |

  I also checked v1 outside Go. `files/etc/systemd/system/agent-bus-hs@.service`,
  `agent-bus-hs-health@.service`, `helpers/agent_bus/render.py` and `agent-bus-install`'s
  restore properties all carry their deny. A Go test cannot reach them, and none is an instance.

- **What this does not cover:** unit text that is not Go, such as a future play's
  `teamchat-tls@.service` template. Its text has to join `madeUnits` when it is made, as the
  table row says.

## X29

The test now reads the terminator's filter the way systemd applies it (`filterLetsIn`): an
address an allow entry covers is let in; otherwise one a deny entry covers is kept out;
otherwise it is let in. It also asserts `SocketBindDeny=any` beside `SocketBindAllow=tcp:<port>`.

The red commit is the mutation that drops the deny. Before the fix, X29 failed with
"198.51.100.7 / 192.0.3.1 / 203.0.114.1 / 203.0.112.255: the unit's filter lets it in = true,
want false" and "the unit's socket bind is not SocketBindDeny=any SocketBindAllow=tcp:8448
alone". After the fix it is green.

**Proven:** the rendered unit text, read under systemd's documented rules, and the recorded
firewall rules.

**Not proven:** a live systemd or firewalld enforcing either one. SPEC X29 and the docs now say
this.

## S2 and S3

`declare.URLProblem(url, port)` is the single rule, and both `readValues` (which covers
`declare check` and every `DecodeInput`) and `host`'s `urlHost` call it. It checks two things:

1. It returns `matrix.CanonicalMemberBase`'s verdict: plain http is allowed only to a loopback IP
   literal (127.0.0.0/8 or ::1); every other url must be https.
2. The url's port must be the team's port. That is the explicit port, or else the scheme's
   default (443 for https, 80 for http).

`declare` now imports `matrix`. The default url is `https://<teamchat_address>:<port>`.

Tests, red then green:

- `declare` `TestPlainHTTPIsForALoopbackLiteralAlone`: red with "err = <nil>, want a FieldError at
  teamchat_teams[0].url". `TestDecodeValid` was red on the http default.
- `host` `TestApplyRefusesPlainHTTPOffLoopback`: red with "got <nil>, want a host Error".
- `declare` `TestURLPortIsTheTeamsPort` and `host` `TestApplyRefusesAURLOnAnotherPort`: both red
  the same way before S3.

I moved test fixtures from http to https and gave a port-less url `:8448`. v1's own `team.json`
fixture keeps http, because that is what v1 wrote.

SPEC X6 and X12 (whose loopback wording is now aligned with the code, review S7's X12 item) and
`docs/agent-bus-cross-network.md` are updated.

## QA

- **9590c875e:** qa-changed FAILED on go test only, on exactly the two intended red tests. Every
  other stage passed.
- **df4078a03:** qa-changed FAILED on one stage, `teamchat-failpoints-test`:
  `member/wiring TestClaudeStatusLine`, in a 616 s package run on a loaded host. This commit
  changes nothing that test touches, and it passes alone (`ok 1.2s`). It is the member/wiring
  timing wait that #193 answer 4 assigns to its own issue. Every other stage passed.
- **be448f45d and c845aecaa:** qa-changed passed.
- `go test -race -count=1 ./internal/teamchat/... ./cmd/teamchat/...` passes with no failure,
  with `./cmd/teamchat-admin/...` added on the last run.
- `go vet` with `-tags busfailpoints` and with `-tags tuwunel` is clean.
- `golangci-lint` over `./internal/teamchat/...` reports 0 issues, both untagged and with
  `-tags busfailpoints`.
- No new project rule, so the rule count and the GoProjectRules catalogue are unchanged.
- I did not run qa-all, as briefed.

## For the PR body and the coordinator

- PR #128's body should say: B3 is fixed in the drop-ins themselves, and X29 now asserts the
  deny half, proven on rendered text and not on a live systemd. The default url is https.
  Non-loopback http and a port mismatch are refused at `declare check` and at apply.
- PLAN.md Task 8.8 should record B3, S2 and S3 as done on this branch. I did not edit PLAN.md.
- **Not verified:** any live systemd or firewalld behaviour, and the tuwunel-tagged tests run
  against a real binary.
- **Skipped:** no plan journal entry was written. Ask if you want one.
