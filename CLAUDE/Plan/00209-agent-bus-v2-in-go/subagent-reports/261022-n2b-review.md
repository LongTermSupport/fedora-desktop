FIX-FIRST

# N2 slice n2b: adversarial review, round 1

Diff reviewed: `git diff feat/bus-v2-n2a..HEAD` on `feat/bus-v2-n2b` (head `f73f2cbc`), judged against
SPEC-n2b (DR, HV, GD, AG, ST, AP, n2b-D1..D11), the later decisions D12-D21 (handoff 2), C 3.5.1
(DC18), 3.6.1, 3.6.2, 3.8, the DC17 and DC18 rows, the N0 re-check findings 4 and 5, gates A and B,
`src-go/CLAUDE.md` and `CLAUDE/GoProjectRules.md`.

## What I ran

| Command (from `src-go` unless noted)                                                                             | Result                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `go test -race -cover ./internal/teamchat/...`                                                                   | all pass; admin 97.3%, apply 92.9%, declare 95.6%, hostvars 96.3%, paths 100%, state 90.7% (others unchanged) |
| `golangci-lint run ./internal/teamchat/...`                                                                      | 0 issues                                                                                                      |
| `go test -race ./godeps/...` (from `src-go/qa`)                                                                  | pass                                                                                                          |
| `FAKEHS_TUWUNEL_BIN=<pinned 1.9.4> go test -count=1 -race -tags tuwunel -run Tuwunel ./internal/teamchat/apply/` | `TestTuwunelApply` passes against the real server (0.5 s), uncached                                           |
| `go list -deps ./cmd/teamchat-admin`                                                                             | no `go.yaml.in/yaml/v3`; the binary now links `apply` and `declare` (for `ExitCodeOf`) but no verb is wired   |
| grep of the diff for `//nolint`, `time.Now`, `time.Since`, `_ =`, `_ :=`                                         | none                                                                                                          |
| grep of `playbooks/`, `tasks/`, `vars/`, `files/` in the diff                                                    | untouched: no install, no unit, no sudoers, no pool password (gates A and B hold; the slice is inert)         |
| three probe tests run through `go test -overlay` (files in the worktree's gitignored scratch, deleted after)     | evidence for findings 1, 2 and 3 below                                                                        |
| `python3 -I` with PyYAML `safe_load` on two YAML snippets                                                        | evidence for finding 2                                                                                        |

What holds, checked and not repeated below: the gate refuses a team with no `approved.json` before
any call (`Hold(nil, …)`, and `Run` dereferences the approval only after it); list fields compare as
sets of validated, distinct entries; `Approve` with `--only` compares every gated field, the named one
included, against the approval with the one change applied, so a planted entry is refused (finding 4
of the re-check is closed); the Element entry takes `team` alone and `base_url` is an unknown key
(finding 5 is closed: no declared URL reaches the profile); `asApproved` takes coordinator, URL and
trust from the approval; the roster event is derived from the file and the approved team; demotion is
saved and converged before promotion; a removed human is marked before the rooms step and locked
after it; a second run makes no write call and no roster write (the engine reports `RosterRev` only on
a write); `state.WriteFile` is temp beside the target with `O_CREAT|O_EXCL|O_NOFOLLOW`, fchmod,
fchown, write, fsync, rename (which replaces a link, never follows it), directory fsync, and removes
the temporary on every failure; `state.ReadFile` refuses links, FIFOs (opened non-blocking) and
oversize files; inside the v2 subtrees the YAML reader refuses tags, anchors, aliases, merge keys,
duplicate keys and every non-JSON plain scalar; more than one document is refused; the keepout row
and its parser rules are as GD1-GD3 say.

## Findings

### 1. major: `team apply` silently erases another operation's `pending` record

`src-go/internal/teamchat/apply/apply.go:327-334` (`finish`) and `:339-342` (`recorder.Intent`).

`finish` sets `Pending = nil` whenever it is non-nil, whatever its `op`, and `Intent` overwrites it.
C 3.6.1 makes `pending` the recorded intent of the one operation in progress, and C 3.6.3 (C20, roll
forward) says any later verb on the team first completes it from its step, setting `needs: reissue` for
an interrupted `add`, `return` or `reissue`. `load` does not look at `pending` at all.

Evidence: a probe test applied alpha, wrote `{"op":"member.add","step":"account","handle":<h>}` into
the roster file, and applied again: the run printed nothing (`changed=[]`) and the file's `pending` was
`null` afterwards. A `member add` that died after its account step therefore loses its only record;
the minted credential's `needs: reissue` is never set, and the board can never show it.

Fix: since this slice builds no roll-forward, refuse before any change (exit 70, step `files` or a
`pending` step) a roster file whose `pending.op` is not `team.apply`, naming the operation and its
step; let `finish` clear only a `team.apply` record, and let `Intent` refuse to overwrite another op.
Test: a pending `member.add` is refused before any request, and the file is unchanged.

### 2. major: a tagged merge key at the top level of host_vars is not refused, and Ansible merges it

`src-go/internal/teamchat/declare/hostvars/hostvars.go:106-114` (`topKey`), line 110.

`topKey` refuses `<<` only when it is a plain scalar. PyYAML (Ansible's loader) decides a merge by the
key's tag, not its text (`flatten_mapping` tests `key_node.tag == 'tag:yaml.org,2002:merge'`), so
`!!merge "<<": {...}`, and `!!merge anything: {...}`, and a `%TAG`-handle spelling of either, are merge
keys to Ansible. The reader ignores them as unknown top-level keys.

Evidence: through `hostvars.Read`, both `!!merge "<<": {teamchat_teams: [{team: alpha, state: absent}]}`
and `!!merge hidden: {…same…}` give zero teams and no error; PyYAML `safe_load` of the same two
documents gives `{'teamchat_teams': [{'team': 'alpha', 'state': 'absent'}]}`. The plain `<<` control
is refused as D19 says.

Impact: n2b-D19 states that a top-level merge key is refused "since either could carry a v2 key not
written as one"; that defence is incomplete. The gated fields still fail closed (the play's step 2 and
`team apply` read Ansible's view and compare it with `approved.json`), but ccy-teams' view of the file
(the board, the approve flows, the HV4 byte-splicing writer of a later slice) is not the play's: a
whole `teamchat_element_teams` list (not gated), or a team's ungated fields, can be supplied invisibly
to the board, and the owner is shown a declaration that is not the one applied.

Fix: in `topKey`, refuse any key with `yaml.TaggedStyle` set, or at least any whose `ShortTag()` is
`!!merge` (which also covers the plain `<<`, since yaml.v3 resolves it to that tag). Add both probes
above as `TestReadRefusesByName` cases, and correct D19's wording in the report.

### 3. minor: an unknown key's name is echoed verbatim, control characters included, so a refusal can forge output lines

`src-go/internal/teamchat/declare/node.go:50` (`n.key(m.Key).fail("is not a known key")`); the same
path building in `declare/hostvars/hostvars.go:158-166` and `apply/files.go:179`.

DR2 and n2b-D11 promise that a declaration error never echoes the value "so the play may print it as
it is". The key text is input too, and JSON (or a double-quoted YAML key) can carry `\n` and `\t`.

Evidence: `Decode` of `{"teamchat_teams":[{"team":"alpha","state":"absent","x\nCHANGED\tforged":1}]}`
returns `declaration: teamchat_teams[0].x\nCHANGED\tforged is not a known key`: a line beginning
`CHANGED<TAB>`, the marker the play reads (C 3.5.1), or equally a forged `SECURITY` line, inside the
text the play prints.

Fix: name an unknown key by `strconv.Quote` (or refuse any key outside `^[a-z_]{1,32}$` as "a key that
is not a known key" with no text), in `declare`, `hostvars` and the roster-file reader; add the probe
as a `TestDecodeRefusesNamingTheField` case asserting no control character in the message.

### 4. minor: `TestApproveRefuses` passes whether or not the change logic refuses

`src-go/internal/teamchat/declare/gate_test.go:395-410` with the cases at `:352-393`.

Every case approves against an input equal to the approval, and only checks for a `RefusedError` with
a problem. Any change that the logic wrongly accepted would change the expected value, and the
mismatch with the unchanged input would refuse it anyway. So about half the cases prove nothing about
the rule they name: "a bad human", "a bad prefix", "a bad address", "coordinator by add",
"coordinator bad handle", "url by add", "url bad", "trust by remove", "trust bad", "lxc bad entry",
"lxc add a present member" (n2b-D21), "lxc set a missing member", "set on a list", "an unknown op".

Fix: for each case, build the input with the change applied (where it can be declared), or assert
`len(re.Differences) == 0` and the problem text, so the refusal is seen to come from the change
check rather than the mismatch. Seeing each case fail with the check removed is the proof.

### 5. minor: Jinja in a host_vars string is read as text here and templated by Ansible

`src-go/internal/teamchat/declare/hostvars/hostvars.go:180-197` (`scalar`).

Ansible templates `{{ }}`, `{% %}` and `{# #}` in host_vars strings when the play renders the team's
stdin, so the play's value differs from what `hostvars.Read` returns. A probe with
`description: "{{ lookup('pipe','id') }}"` is accepted as that literal string. Gated fields fail
closed (their grammars mostly refuse braces, and the gate compares the play's view); the ungated
`name`, `description`, `checkout` and `site` reach `team.json`, the room names and topic with whatever
the template produced, while the board shows the template text. The same divergence class as 2.

Fix: refuse a v2 string holding `{{`, `{%` or `{#` in `hostvars` (named, "is templated by Ansible"),
or record the decision that it is K9's and accept it; either way state it in C 3.8 / HV2.

### 6. nit: an existing account not in the roster file is adopted with no line printed

`src-go/internal/teamchat/apply/apply.go:231-232` (n2b-D14).

A declared human whose account already exists but is not in the file is recorded `active` with
`revoked_seq` 0 and prints nothing, then invited to every room. Apply did not create that account and
does not know who holds its password. The case is rare (a lost file, an account made with the
registration secret), but it should leave a trace: print `human <name> adopted` (a CHANGED line), or
refuse naming `human password`.

### 7. nit: no `Run`-level test of a team that has no `approved.json`

`src-go/internal/teamchat/apply/apply_test.go:345-362`.

`TestADeclarationNotApprovedIsRefusedBeforeAnyCall` covers a changed declaration and an unreadable
approval, but not `Load` answering nil (a team never approved), which the brief asks about and AP5
names. `TestHold` covers it in `declare`; add the nil case to the `Run` test, asserting step `gate`,
drift, the `SECURITY\talpha\tteam\t- -> new` line and zero requests.

## Count

7 findings: 2 major (1, 2), 3 minor (3, 4, 5), 2 nit (6, 7). No blocker: the DC18 gate itself holds
against every bypass tried. Fix 1-4 before merge; 5 needs a fix or a recorded decision.
