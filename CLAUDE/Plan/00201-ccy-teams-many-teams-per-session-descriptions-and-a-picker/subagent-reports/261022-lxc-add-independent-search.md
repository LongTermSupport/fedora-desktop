# Independent search: Class A (retry on config-file state) and Class B (non-idempotent add)

The dispatch named the classes and the originating instances only; it did not contain the detector's rule or pattern.
I did not open src-go/qa/rules/ or src-go/qa/driver/ and ran no detector.
Paths are relative to the worktree root.

## Expected forms (written before searching)
- A: a bounded loop (for range maxTries, for attempt) around an input prompt whose checker calls declare.py and prints "Try again" for any *declare.Error.
- A: the checker is judged against the file, not just the answer.
- B: add/create/append commands that parse existing + new and raise on a duplicate, instead of answering UNCHANGED.

## Searches run
- "Try again|Giving up|tries|attempts" over go/py/bash/j2/yml, excluding src-go/qa/rules and src-go/qa/driver.
- "maxTries|MaxTries|maxAttempts|for attempt|for range max" in src-go/internal.
- "bufio.NewReader|ReadString|Scanner" to find prompt readers.
- Callers of every declare client mutator: AddRepo, LxcAdd, Create, Element, Describe, DropLimit, Set, LxcSetRole, the DryRun variants.
- "ErrUsage|ErrRefused|errors.AsType[*declare.Error]" to see which loops discriminate the error kind.
- "^def (add|append|create|insert|...)" in helpers/.
- "read -p|read -rp|input(" in files/**/bin and helpers.
- agent-bus-* files, files/var/local/claude-yolo/lib/agent-bus-*.bash and the ccy lib: no retry loops.

## Which loops discriminate the error kind
The Go declare client maps declare.py exit 64 to ErrUsage and 78 to ErrRefused (src-go/internal/declare/declare.go:34-63).
In declare.py, check_lxc raises UsageError (exit 64) for a duplicate, a second coordinator, a seat clash and a two-hosts conflict.
It does so by re-wrapping members.MemberError at helpers/agent_bus/declare.py:1228-1230.
So exit code 64 vs 78 cannot separate "bad input" from "file state" for these conflicts. Only the message separates them.
- Discriminate (ErrUsage retries, anything else ends the run):
  - src-go/internal/teams/picker/offers.go:304 (inputDescription)
  - src-go/internal/teams/settings/settings.go:291 (CheckSet loop)
- Do NOT discriminate (any *declare.Error is retried with "Try again"):
  - src-go/internal/teams/lxcadd/adder.go:544-554 (checked)
  - src-go/internal/teams/create/wizard.go:705-715 (checked)
- The discriminating loops are not protected either: because the code is 64 for conflicts, a file-state UsageError would still be retried there.

## Candidates

### A1. lxcadd askUser -> askValid/checked (ORIGINATING). CARRIES THE HAZARD, confident.
- src-go/internal/teams/lxcadd/adder.go:418-444 (askUser), 436 (LxcCheck with team+container+answer), 513-530 (askValid), 544-554 (checked).
- Technique: text search ("Try again") and reading.
- declare.py check_lxc (1224-1230) judges the entry among the existing agent_bus_lxc_members entries.
- members.parse_members raises these as UsageError, exit 64 (helpers/agent_bus_lxc/members.py around 520-536):
  a duplicate user@container in the team (L520), a container with two hosts (L524), a seat clash (L531), a second coordinator (L536).
- Retyping the same user cannot succeed. The message names entry numbers but not the file or the entry to edit.
- Only a different user name can pass, and the question says the user "must already exist".
- Nothing discriminates the kind. Three tries are burned, then "Giving up after 3 tries".

### A2. lxcadd askCheckout loop, via checkoutOK -> LxcDerive. UNSURE, probable hazard.
- src-go/internal/teams/lxcadd/checkout.go:51-70 (loop), adder.go:448-464 (checkoutOK), adder.go:459 (checked(LxcDerive)).
- Technique: reading. Text search found only the "Giving up" line.
- helpers/agent_bus/declare.py:1244-1264 (derive_lxc) resolves the new entry among the already-declared entries.
  It keeps those that are state absent or have both host and seat (L1256); members.MemberError becomes UsageError (L1263-1264).
- A seat or host clash, a duplicate or a two-hosts conflict with an existing entry is shown as "Try again" and the PATH is asked again,
  with the value reset to the path's directory (checkout.go:67).
- Mostly shadowed by A1, which refuses a duplicate earlier.
- A different checkout path may change the repo and so the seat, so it is only sometimes unrecoverable.
- The roster/probe errors in the same call (derive.DeriveError) are input or container problems and are legitimately retried.

### A3. create wizard checkers. UNSURE, a lesser instance.
- src-go/internal/teams/create/wizard.go:558-564 (askTeam -> Check with Team), 705-715 (checked).
- Technique: reading.
- _check_flags (declare.py:756-762) refuses "--team: team X is already declared in agent_bus_teams" (UsageError).
  It is a conflict with an existing file entry, retried with "Try again". Retyping the SAME name cannot pass, but a different name can,
  so whether it counts depends on how the rule defines "same question". The message does not say which file to edit.
- The other wizard checks (description, humans, allow_from, repos, owner) judge the answer by its own form only.
  The check command still parses the file first (declare.py:778), so an unparseable file raises DeclareError (78),
  which the non-discriminating checked would retry. Mitigated: the wizard reads Declare.Address before the first question
  (wizard.go:345), which fails on an unparseable file, so the parse case is unlikely to reach the loop.
- askAddress (657-659, check-address) is a pure form check and carries no file state.

### A4. settings CheckSet loop. UNSURE, edge.
- src-go/internal/teams/settings/settings.go:270-295.
- Technique: reading.
- Discriminates ErrUsage vs other, so a parse failure (78) ends the run correctly.
- But two file-state refusals are UsageError and are retried:
  _present (declare.py:813-817) "--team: team X is not declared present, with a port";
  set_values (declare.py:1123-1125) judges the whole entry after the change with teamfile.parse_team_file,
  so a pre-existing invalid value elsewhere in the entry is refused with the same message on every try.
- Both need the file to have changed after the screen opened, or to hold an already-invalid entry, so they are rare.

### A5. picker inputDescription. NEAR-MISS, mostly safe.
- src-go/internal/teams/picker/offers.go:288-312.
- Technique: text search, then reading.
- Discriminates ErrUsage. The only UsageError reachable for a description is field_error (declare.py:764-767), an input check.
- A parse failure is 78 and ends the run. Safe, apart from the exit-64 caveat above.

### B1. declare.py add_lxc / check_lxc / lxc-add (ORIGINATING). CARRIES THE HAZARD, confident.
- helpers/agent_bus/declare.py:1274-1286 (add_lxc), 1208-1230 (check_lxc), 1380-1386 (command).
- Technique: reading plus text search for lxc-add.
- add_lxc runs check_lxc, which judges existing + new together. Re-running for an entry already present raises
  "entries N and M both declare user@container" (exit 64). It never answers UNCHANGED, unlike lxc-role, lxc-remove, describe, element and set.
- The Go caller is half prepared for it and half not:
  - src-go/internal/teams/lxcadd/adder.go:711-722 handles Changed / UNCHANGED, but lxc-add never emits UNCHANGED, so that branch is dead.
  - The dry-run at adder.go:564-569 refuses the second run with "Refused; nothing was changed", which is false because the entry is already in the file.
  - A first run whose apply failed (adder.go:723-731) tells the person to run "ccy-teams apply <team>". A second lxc-add is refused at the user question (A1) and again at the summary.

### B2. declare.py create (new team). CARRIES THE HAZARD, confident for the non-interactive path; unsure for the wizard.
- helpers/agent_bus/declare.py:782-810, _check_flags 759-760 ("already declared"), and 791-792 ("--address ... is already set").
- Technique: reading.
- Create appends the entry and refuses a team that is already declared. It never answers UNCHANGED.
- Go callers: src-go/internal/teams/create/wizard.go:827 (dry-run), 893 (Create). declareAndApply calls Create, then Element, then ApplyTeams.
  When the install fails (errInstallFailed, wizard.go:872-881) the person is told to run "ccy-teams apply".
  A re-run of create for the same name is refused at askTeam (A3) or, with --yes, by CreateDryRun.
  After the first team wrote the address, a re-run with --address is refused.
- The Element step is idempotent (UNCHANGED).

### B3. declare.py add_repo. UNSURE, mitigated by the caller.
- helpers/agent_bus/declare.py:942-982, 950-951.
- Technique: reading.
- Re-adding a repo the team already trusts raises UsageError "already trusts"; it does not answer UNCHANGED.
- Caller src-go/internal/teams/chooser/widen.go:114-129 only offers teams whose standing is WIDEN, computed from the declared file,
  so a re-run after a failed apply does not offer the team again. The hazard only appears for a direct declare add-repo re-run.

### B4. declare.py drop_limit. UNSURE, minor.
- helpers/agent_bus/declare.py:985-993.
- A re-run raises UsageError "sets no path_prefixes" instead of UNCHANGED.
- Caller src-go/internal/teams/picker/offers.go:149-153 only asks about teams listed as limited, and reports the error as a failure (no retry).
- It is a removal, not an add, so it is a weak match for B.

### B5. helpers/agent_bus/admin.py add_member. NEAR-MISS.
- helpers/agent_bus/admin.py:1005-1050. Technique: reading.
- It claims a handle in the registry, then creates the account, and rolls back on failure.
  It refuses a seat that is a current unparked member, and an account that already exists.
- A different mechanism (registry plus homeserver call), not append-then-validate-the-whole-list. Without --seat a re-run mints a new handle.
- Not an interactive retry.

## Idempotent controls (checked, not instances)
- set_lxc_role and mark_lxc_absent (declare.py:1289-1341) return None (UNCHANGED) when the state already holds.
- set_description (describe), set_element (element) and set_values (set) return None (UNCHANGED).

## Near-misses examined and ruled out (retry loops with input-only refusal)
- src-go/internal/ccproject/app.go:167: Y/n parse only.
- src-go/internal/freeze/chooser.go:171,246 and src-go/internal/open/choose.go:112: numbered menus, input range only.
- src-go/internal/sessions/picker.go:103: bad picks.
- src-go/internal/teams/create/wizard.go:455-463: repos, owner (checkOwner) and checkRepos check form and GitHub only.
- src-go/internal/teams/ops/takedown.go:200: a typed team name, no retry.
- files/home/.local/bin/ssh-copy-id-with-key:79: one read, no retry.
- The bash ccy lib and the agent-bus-* files have no retry prompts.

## Attribution to technique
- Text search alone found:
  - the "Try again"/"Giving up" strings in lxcadd/adder.go:536 and :528, create/wizard.go:697 and :689, lxcadd/checkout.go:70, picker/offers.go:311
  - the maxTries constants
  - the ErrUsage discrimination sites (picker/offers.go:304, settings/settings.go:291)
- Reading was needed, and text search could not have shown it, for:
  - A1: that checked retries on every *declare.Error, and that check_lxc judges the entry among the existing ones
  - A2: that checkoutOK reaches LxcDerive, which resolves against the existing entries, from the checkout loop
  - A3 and A4: the _check_flags, _present and set_values file-state refusals
  - that duplicate, second-coordinator and seat-clash conflicts are exit 64 (UsageError), so ErrUsage vs ErrRefused does not separate them
  - B1 to B4: that the add commands never answer UNCHANGED, and that the Go UNCHANGED branch in adder.go:716-722 is dead for lxc-add
  - the mitigations: the wizard reads the declared address first (wizard.go:345), lxcadd reads LxcMembers first (adder.go:239), widen only offers WIDEN teams
