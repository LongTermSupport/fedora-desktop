# Plan 00161: acceptance check that room creation is refused (M1.7)

Added M1.7 to `acceptance.bash` (after M1.6). `check_room_creation` in
`_acceptance-steps.inc.bash` runs as member b's own transient service: it reads the token and
`base_url` from the bundle, POSTs `/_matrix/client/v3/createRoom`, and prints one JSON object.
`acceptance_check.py` gains `expect_room_refused` and the `room-refusal` command.

- PASS only on 403 `M_FORBIDDEN` (Tuwunel 1.9.3 `can_create_room_check`, "Room creation has been
  disabled", cited in the check's comment and DESIGN.md section 7). Status and errcode are
  printed and written to the report evidence.
- FAIL on a created room (the probe leaves and forgets it first), or on any other answer, such
  as 401, which proves nothing about the setting.
- Verified here: bash -n and shellcheck clean on both bash files; ruff clean; the two new
  unit tests pass; the inline probe was run against a stub server for the refuse path and the
  create path, which made the leave and forget calls. One existing failure,
  `TeamFileTest.test_it_passes_the_admin_tool_validator` (forge_api), is not from this change.
- Not run: the host run. PLAN.md and JOURNAL/ untouched, per instructions. M1.7 reaches the
  host through the existing meta-deploy acceptance leg.
