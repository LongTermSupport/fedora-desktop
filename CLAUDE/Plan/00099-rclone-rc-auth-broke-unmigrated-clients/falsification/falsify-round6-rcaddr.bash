#!/usr/bin/env bash
# Round 6, blocking: five copies of the same line died under `set -euo pipefail` when a
# mount published no `--rc-addr`, because grep exits 1 on no match and an assignment from a
# failing pipeline triggers errexit. The guard written for exactly that case was unreachable
# in all five.
#
# This drives each site's real block — lifted from the shipped file, not retyped — against a
# cmdline WITHOUT --rc-addr (the mutant scenario) and one WITH it (the control), at top level
# under the same options the scripts set.
#
# The control matters as much as the mutant: an assertion that fails on everything proves
# nothing about the fix.
set -euo pipefail

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_paths.inc.bash
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/_paths.inc.bash"
BIN="$BIN_SRC"
WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

# Lift each site's `case` arm verbatim. Anchors asserted, so a moved boundary fails loudly
# rather than quietly testing an empty string.
python3 - "${WORK}" "${GATE}" "${BIN}" << 'PY'
import os, sys

work, gate, bindir = sys.argv[1], sys.argv[2], sys.argv[3]

SITES = {
    "acceptance.bash": (gate, 'if rc_addr_raw=$(grep', '$rc_cmdline', 'RC_ADDR'),
    "rclone-cache-warm": (os.path.join(bindir, "rclone-cache-warm"), 'if rc_addr_raw=$(grep', '$cmdline', 'RC_ADDR'),
    "rclone-cache-status": (os.path.join(bindir, "rclone-cache-status"), 'if rc_raw=$(grep', '$cmdline', 'rc_addr'),
    "rclone-tail": (os.path.join(bindir, "rclone-tail"), 'if rc_raw=$(grep', '$cmdline', 'rc_addr'),
}

for name, (path, anchor, cmdvar, outvar) in SITES.items():
    src = open(path).read()
    assert anchor in src, f"{name}: the captured-assignment anchor is gone"
    start = src.index(anchor)
    # Take through the closing `fi` of the else-branch that sets the empty value.
    end = src.index("fi\n", src.index("else", start)) + len("fi\n")
    block = src[start:end]
    assert "else" in block and "grep -oE" in block, f"{name}: extracted the wrong block"

    harness = (
        "#!/usr/bin/env bash\n"
        "set -euo pipefail\n"
        f"{cmdvar[1:]}=\"$1\"\n"
        + block +
        f'printf "REACHED the guard; {outvar}=[%s]\\n" "${{{outvar}}}"\n'
        f'if [ -z "${{{outvar}}}" ]; then printf "  and it is EMPTY, so the caller can report it\\n"; fi\n'
    )
    open(os.path.join(work, name + ".bash"), "w").write(harness)
PY

WITHOUT="rclone mount remote:bucket /mnt/thing --vfs-cache-mode full --config /dev/null"
WITH="rclone mount remote:bucket /mnt/thing --rc --rc-addr=localhost:5573 --vfs-cache-mode full"

run_site() {
    local site="$1" cmdline="$2" out rc=0
    # `if cmd; then rc=0; else rc=$?; fi` — NOT `if ! cmd; then rc=$?`, which records 0 for
    # every failure because `!` inverts the status before `$?` is read.
    if out="$(bash "${WORK}/${site}.bash" "${cmdline}" 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    printf '    %-22s exit=%-3s %s\n' "${site}" "${rc}" "${out//$'\n'/ | }"
    return "${rc}"
}

SITES=(acceptance.bash rclone-cache-warm rclone-cache-status rclone-tail)

printf 'SHIPPED — no --rc-addr in the cmdline (the scenario that used to kill the run):\n'
shipped_ok=1
for site in "${SITES[@]}"; do
    if ! run_site "${site}" "${WITHOUT}"; then
        shipped_ok=0
    fi
done

printf '\nCONTROL — --rc-addr present, so the address must come back resolved:\n'
control_ok=1
for site in "${SITES[@]}"; do
    if ! run_site "${site}" "${WITH}"; then
        control_ok=0
    fi
done

# The mutant: the pre-fix line, reconstructed. Not lifted from the file, because the file no
# longer contains it — so it is written here and its shape stated, which is the honest form.
cat > "${WORK}/mutant.bash" << 'MUT'
#!/usr/bin/env bash
set -euo pipefail
cmdline="$1"
RC_ADDR=$(grep -oE -- '--rc-addr=[^ ]+' <<< "$cmdline" | head -n1 | cut -d= -f2)
printf 'REACHED the guard; RC_ADDR=[%s]\n' "$RC_ADDR"
MUT

printf '\nMUTANT — the line as it shipped before this round, same two inputs:\n'
if mut_out="$(bash "${WORK}/mutant.bash" "${WITHOUT}" 2>&1)"; then
    mut_without_rc=0
else
    mut_without_rc=$?
fi
printf '    %-22s exit=%-3s %s\n' "no --rc-addr" "${mut_without_rc}" "${mut_out:-(no output at all)}"
if mut_out2="$(bash "${WORK}/mutant.bash" "${WITH}" 2>&1)"; then
    mut_with_rc=0
else
    mut_with_rc=$?
fi
printf '    %-22s exit=%-3s %s\n' "--rc-addr present" "${mut_with_rc}" "${mut_out2}"

printf '\n'
if [ "${shipped_ok}" -eq 1 ] && [ "${control_ok}" -eq 1 ] \
    && [ "${mut_without_rc}" -eq 1 ] && [ "${mut_with_rc}" -eq 0 ]; then
    printf 'MUTANT KILLED — the pre-fix line exits 1 with NO OUTPUT when the flag is absent,\n'
    printf 'so the error message written for that case never printed. All four shipped sites\n'
    printf 'now reach their guard with an empty address, which is what lets the caller say so.\n'
    printf 'The control proves none of them simply returns empty for everything: with the flag\n'
    printf 'present, every site resolves localhost:5573.\n'
    exit 0
fi
printf 'NOT ESTABLISHED (shipped=%s control=%s mutant_without=%s mutant_with=%s)\n' \
    "${shipped_ok}" "${control_ok}" "${mut_without_rc}" "${mut_with_rc}"
exit 1
