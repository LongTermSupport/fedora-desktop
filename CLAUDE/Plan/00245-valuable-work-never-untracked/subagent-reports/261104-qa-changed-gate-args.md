# qa-changed ran gate scripts without their qa-gates.tsv arguments

Plan 00245, Defence Before Fix.

## Class

qa-changed runs a gate's script without the arguments its `scripts/qa-gates.tsv` row gives it.
`rowsByScript` kept the rows only to judge the output; the run itself used the bare script
(`session.script`). A script that needs its arguments printed usage and exited 2.

## Detector (red first)

`src-go/qa/changed/suite_args_test.go`:

- a fake row with arguments: the executed argv must carry them and the bare script must not run;
- a script with two rows: it runs once per row;
- every row of the real `qa-gates.tsv` whose command has arguments is run with them.

All three failed before the fix (commit "Red: ...").

## Sweep

Rows of `qa-gates.tsv` with arguments: `private-names` (`qa-private-names.bash tree HEAD`), the only
one that runs a script under `scripts/`, so the only one silently broken in qa-changed. The two
`python3 -m helpers.gnome...` rows are not scripts, so they are not suites. No script has several
rows today; the code and a test handle it anyway.

## Fix

`suiteRuns` in `src-go/qa/changed/run.go`: a picked suite runs once per row, as
`bash <script> <row arguments>`, each judged by its own row's readers. The golden replay and
`scripts/test-qa-changed.bash` needed no change (no deviation: the golden never ran a row with
arguments).
