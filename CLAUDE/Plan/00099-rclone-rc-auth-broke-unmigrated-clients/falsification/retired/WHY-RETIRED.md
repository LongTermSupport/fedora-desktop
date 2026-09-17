# Retired harnesses — they pass, and what they vouch for no longer exists

`falsify-check6-input.bash` and `falsify-check6-input2.bash` test check [6]'s old
stand-in inputs: `"$rc_mount/."` and a one-level subdirectory. Check [6] no longer uses
either — it invokes `ftp-camera --copy-preflight`, the client itself.

Both still run green. That is the problem: a harness that passes while testing a code
path the gate abandoned is a green result carrying no information, which is this plan's
own defect class pointed at its own tooling. Moved here rather than deleted, because the
mutant tables in them are the evidence for why the stand-in approach was abandoned.

Live harnesses for check [6] are `falsify-round4-fixes.bash` (the depth mutant, and the
structural assertions) and `falsify-round5-note.bash` (which EXECUTES the block — the
round-4 one only greps its text, which is how an undefined `note` shipped).
