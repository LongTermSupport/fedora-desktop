# Review round 1: fix/acceptance-checker-a3-m3 (Plans 00211 A3, 00201 M3)

Branch `fix/acceptance-checker-a3-m3`, four commits over F44 (bd3cf6661..a0b04b9d7).

## Verdict: MERGE

No blocking defect. Both fixes target the right subject and should pass on a real host, given
the stated precondition (the owner has accepted a host seat beside Element at `cc --teams`).

## What was verified

### A3 (Plan 00211)

- The host `/usr/local/bin/pingbus` is the Python zipapp of `helpers/pingbus`
  (`scripts/test-agent-bus-install.bash:436`), so `config.load_bundle` is the code that judges
  A3.
- `agent-bus add-member --out=DIR` (files/usr/local/bin/agent-bus): it refuses an existing
  `--out`, and runs `install -d -m 0700` and the file writes as `SUDO_UID` through setpriv. The
  new `A3_DIR` (`$PLAN_RUN_DIR/a3-seats/<team>/<tag>`) does not exist and its parents are made
  0700 by the user, so the link target is a real directory, the user's, mode 0700. That is
  exactly what `_check_linked_bundle` requires.
- `ln -s -- "$A3_DIR" "$A3_HOME/$QTEAM"`: `PLAN_RUN_DIR` is absolute (under
  `PLAN_REPO_ROOT`), so the link is absolute, which `link.parent / readlink` resolves
  correctly. It is not a link onto a link.
- `env -i ... PINGBUS_TEAMS=` with no `PINGBUS_MEMBERSHIP`, so `active_teams` takes the env
  path, as before the fix. State goes to `bundle_dir/state`, which is under the seat
  directory through the link. `auth.header` and `answer.json` sit beside the bundle as they
  did before.
- `element_accepted` is written only by the Go chooser's consent (`cc --teams`, a host
  launch, from a terminal: consent.go). The new A3 failure message and the meta-deploy text
  say exactly that. With no Element profile at all, the seat passes anyway.
- `a3_release`: unchanged and correct. It is keyed on `A3_DIR/member.json`, which still
  exists. The token is left in `untracked/plan-runs`, and the member is deactivated, the same
  as before.
- A4 and the test question: I read them. They run through `session_run` inside the member's
  own container or guest, on bundles that production placed. The output fields (SENT $4,
  QUESTION $4) match `cli.LINES`. They are sound.
- `--stage 2 --fake`: rc 0, all legs OK, and both new A3 legs PASS.

### M3 (Plan 00201)

- The subject is right. `/tmp/pingbus-home/settings.json` in a seated ccy session is written
  by Python `pingbus seat exec` (entrypoint.sh:593, `seat.write_session_files`). The image's
  zipapp is built from this checkout, so deriving the expectation from `seat.session_settings`
  is correct.
- The check is not tautological: the session file under test comes from the container. The
  guard-ends-with-kit-command cross-check ties seat.py to the kit's `settings.json`. Comparing
  every key that seat.py sets, with `padding` allowed extra, is exact without being brittle.
- Under `python3 -I` from `/tmp`, `expect-session-settings` passes on the settings seat.py
  writes (rc 0). `session-status-line` (run in the container) still imports nothing from the
  checkout at module level.
- `goodbye_check.py`: the module-level imports pull in only stdlib plus `helpers.*`. Run as
  `python3 -I` from `/tmp` it works. It runs on the host (`GB_CHECK`).
- Tests: the rule tests (12), `test_m7_check.py` (49) and `test_goodbye_check.py` (34) are all
  OK.

### Other

- Public-repo hygiene: no host-specific names, paths or emails in the diff.

## Findings (none blocking)

1. LOW: in the A3 fake leg, the `reject_leg ... fake_a3_seat unaccepted` passes on any
   failure. It does not assert that the refusal is the Element refusal. The accepted leg
   limits this in practice. Matching the "must not run as a user with an Element profile"
   text would close it.
2. LOW: the A3 fake leg calls `config.load_bundle` directly, not the CLI path that
   `pingbus report` takes with `env -i` and `PINGBUS_TEAMS`. That path is unchanged from the
   host run, which reached `load_bundle`, so the risk is small.
3. LOW (rule false negative): `checker-copies-cited-constant` sees only copies whose comment
   names the product constant. `_acceptance-m7.inc.bash:39`
   `readonly M7_CTR_PINGBUS_HOME="/tmp/pingbus-home"` cites seat.py but not `SESSION_HOME`, so
   it escapes. It is documented as referred (the independent search "Weak INSTANCE", Task
   2.21). It is acceptable as declared.
4. LOW (rule false negative): `fixture-plain-member-bundle` does not follow `--out DIR` (the
   wrapper refuses that form anyway), a home passed through an env array, or a default
   `PINGBUS_HOME` (`~/.config/pingbus`). It is sound for what it claims.
5. LOW (plan state): Plan 00211 Task 2.10 is marked `[x] ✅` while its host rerun is still
   queued. Plan 00201 Task 2.21 marks the same situation `🔄 ... left: the host rerun`. 2.10
   should match: in progress until meta-deploy passes A3.
6. INFO: the meta-deploy.list descriptions are accurate (the entry kinds match the header,
   and the precondition and its remedy are stated). The 00201 entry's "acceptance.bash only"
   is true: the folder has no triage.bash or deploy.bash.
