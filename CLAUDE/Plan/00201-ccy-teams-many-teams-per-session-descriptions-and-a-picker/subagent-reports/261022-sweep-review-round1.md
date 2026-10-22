# Task 2.22 sweep review, round 1 (adversarial)

Branch `fix/checker-owned-values-sweep`, tip `9001c6a3a`, reviewed as `git diff F44...HEAD`
(F44 `89e0a1a71`). Nothing in the branch was changed.

**Verdict: MERGE.** No blocking finding. Three non-blocking findings are below.

## What was run

- `./scripts/qa-changed.bash --base F44`: exit 0.
- pytest over `test_m7_check.py`, 00161 `test_triage_probe.py` and `test_acceptance_check.py`,
  `tests/helpers/plan_acceptance/test_cited_constant_copy_rule.py` and
  `tests/helpers/agent_bus/test_declare.py`: all 368 pass.
- `go test ./internal/teams/hot/ ./internal/teams/membership/`: ok.
- 00194 `acceptance.bash --fake`: FAKE ACCEPTED. It runs `gb_judge`'s new `product-value`
  read of `GOODBYE_REASONS[prompt_input_exit]`.
- 00211 `acceptance.bash --fake --stage 2`: FAKE ACCEPTED. It runs the new source-time reads,
  `INBOX_NAMES`, `TEAM_STATE_DIR` and the Element fixture paths.
- The rule's `sweep()` at the tip: 0 findings.
- Every new read, run by hand against this checkout, gives the literal it replaces:
  - `CHECKOUT_SEATS`, `STATE_ROOT`, `INSTALL_STATE_ROOT`, `APPLIED_DIR`, `HEALTH_DIR`
  - `SESSIONS_DIR_TEMPLATE`, `MEMBERSHIP_VAR`/`MEMBERSHIP_OFF`
  - `session-paths`, `session-names`, `membership-dirs` (`ccy/membership`,
    `pingbus/membership`, `relaunch.json`)
  - `TEAM_NAME_PATTERN`, which is a valid bash ERE as `^${team_name}$`
  - `seatsDir`, `STATE_DIR`, the inbox names, `ELEMENT_FLATPAK_DIR`, `removedHomeserverDown`
  - `GOODBYE_REASONS[prompt_input_exit]` gives `exit`
  - the 00128 census uses `verdicts.STAGE`, the same grammar as the old grep
  - m2-check's `\${PINGBUS_MEMBERSHIP-` is present in `entrypoint.sh:578`

## Hunted and not found

- **Silent reads.**
  - Every source-time read is a plain `X="$(...)"` at top level, under errexit:
    - the m7 inc's `m7_value` reads
    - 00211 stage 2, one substitution per `+=()`; proven to stop under `set -e`
    - 00203 `triage-hs.bash`
    - `m2-check.bash` `names`, which calls `fail` and exits
  - Every function-time read has its own `|| return 1`:
    - `m3_value` / `s1_value` (both use `printf -v`, and no caller passes their `text`)
    - `u20_lines`
    - `gb_paths`
    - `watcher_logs`
    - `c5_cpu`
  - Nothing reads where errexit is suspended unguarded.
- **Ordering.**
  - 00203 reads its names (`m3_read_names`) in the first leg, before any
    `plan_on_cleanup`, so the `${M3_*:?}` uses in the M2, M4, M6 and robust cleanups
    cannot expand empty.
  - 00161 `u20_read_names` runs before `u20_prerequisites`, the first user of
    `U20_MOUNT`. `u20_teardown_after_stop` is gated on `U20_STARTED`, and
    `u20_named_down` now skips an empty `U20_NAMED_DIR`.
  - 00194 defines `CHECK` before both modes.
- **Quoting and `grep -P`.**
  - `\\Q${M3_MARK_DONE}\\E` inside double quotes gives `\Q✓\E`.
  - The other `-E` to `-P` patterns (`[0-9]+`, `\(`, `\.`, `$`) mean the same in PCRE.
  - `removedHomeserverDown` has no `\E`.
- **Reads inside containers.** The in-container Python (`M7_RECORD_PY`, the SessionEnd
  watcher, `GB_RECORD_PY`, `WHERE_PY`) is handed the value as an argument and imports
  nothing from the checkout. `triage_probe.py` is run on the host, and it already read the
  checkout's pin. `declare.py` imports only stdlib and `helpers/`.
- **Product code.**
  - `remove.go` and `membership.go` are pure renames: `filepath.Join` with
    `"ccy/membership"` gives the same path, and `remove_test.go` asserts the text.
  - `declare.py`'s one behaviour change, refusing an empty version, is stated in the
    changelog and tested.
  - The CCY bump (3.100.13, container 2.79.8; Dockerfile label and
    `REQUIRED_CONTAINER_VERSION` match) is justified because `declare.py` is in the image.
  - `meta-deploy.list` only renumbers the comments of existing entries. The converted
    checkers run inside acceptance runs that are already queued.
  - `ccy-teams` reads the pin through `declare.py status` (Python), so the changelog's
    "one reader" is true.

## Non-blocking findings

1. **`product_value` ignores `[key]` on a value that is not a table.** In `m7_check.py`
   `product_value`, a key is applied only `if isinstance(value, dict)`, so on a string the
   whole string is returned. Verified:
   `product-value helpers/pingbus/statusline.py 'SESSIONS_DIR_TEMPLATE[uid]'` prints
   `/tmp/pingbus-sessions-{uid}`, exit 0. A mistyped checker call therefore passes a wrong
   value instead of failing. No current caller does this. Fix: fail when a key is given and
   the value is not a dict holding it. Add the case to `test_a_product_value_is_read_by_name`.
2. **The Go reader's pattern also matches function-local assignments.** The pattern is
   `^(?:const\s+|\s+)NAME(?:\s+string)?\s*=\s*"..."` (in both `product_value` and the rule's
   `go_constant_value`). It also matches an indented assignment inside a function body
   (`\tNAME = "x"`). If such a line shares a constant's name, it either doubles the match
   (a loud failure) or, if the constant is gone, is read in its place (a silent wrong read).
   This is latent: no such line exists today. Anchoring to `const` lines and `const (...)`
   blocks would close it.
3. **A reader failure in 00194's fake mode counts as a rejection.** The reject leg
   "G1 (fake) a goodbye with another reason" passes if `gb_judge`'s `product-value` read
   fails. The G1 and G2/G3 `check_leg`s call the same read and would fail, so the fake run
   is not silently green. Only that one reject leg's PASS would be meaningless.

## Plan and report claims

- Spot-checked against the code, and true:
  - the converted site list
  - 0 findings at the tip
  - the readers the report names
  - "the Tuwunel pin has one reader"
  - the bumps
- The PLAN.md diff also inserts blank lines between the earlier task bullets (the
  formatter). This is harmless.
- The referral "`.config/pingbus` is not named by the product" holds for `config.py`
  `home()`, which builds it inline. `config.py` `ELEMENT_ACCEPTED_DIR` names the same text
  for another purpose, and the branch reads it. The report could say so, to stop a later
  sweep from reusing it for the LXC pingbus home in 00211 `session_state`.
