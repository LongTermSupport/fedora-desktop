#!/usr/bin/env bash
# Reproduces the QA-tool pin failure under pipx's uv backend, and proves the fix, by running
# play-python.yml's own QA-tool tasks (probe, remove, install, pin) through ansible and the
# real community.general.pipx module, against a real pipx and a real uv.
#
# The tasks are lifted out of the play unchanged: "before" from --before-ref (default
# fadc1eca, the F44 commit the failure was seen on), "after" from the working tree. Each case
# gets its own HOME, PIPX_HOME and PIPX_BIN_DIR in a temp directory, and PATH holds only that
# bin dir, uv and /usr/bin:/bin, so no ruff or semgrep installed elsewhere is probed.
#
# ruff and semgrep are FAKE wheels built here, one off-pin version and one pinned version, and
# uv resolves them offline (UV_NO_INDEX + UV_FIND_LINKS). Nothing is downloaded and nothing
# outside the temp directory is written. The pre-states are made with the same pipx, and each
# asserts that pipx recorded the uv backend, since pipx honours a venv's recorded backend
# over its default and a pip-backed venv would not reproduce the failure.
#
# Needs: a python whose `-m pipx` is a pipx with the uv backend (Fedora 44 ships one), uv on
# PATH, ansible-playbook, and community.general (ANSIBLE_COLLECTIONS_PATH is passed through).
# The play's pipx tasks run `<target python> -m pipx`, so --pipx-python becomes the
# inventory's interpreter.
#
# Ansible runs sealed, never through plan_ansible_playbook: that wrapper uses the repo's
# ansible.cfg, whose play_ledger callback would record these runs in the machine's ledger and
# which needs the vault password file. Every run targets localhost with throwaway config,
# inventory and temp directories, as scripts/test-git-signing-declared.bash does.
#
# Usage: reproduce-pipx-uv.bash --pipx-python <python> [--before-ref <git ref>]
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each
# result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="${SCRIPT_DIR}"
while [[ "${REPO_ROOT}" != "/" ]] && [[ ! -e "${REPO_ROOT}/ansible.cfg" ]]; do
    if [[ -e "${REPO_ROOT}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${SCRIPT_DIR}" "${REPO_ROOT}" >&2
        exit 1
    fi
    REPO_ROOT="$(dirname "${REPO_ROOT}")"
done
if [[ ! -e "${REPO_ROOT}/ansible.cfg" ]]; then
    printf '[FATAL] no ansible.cfg above %s\n' "${SCRIPT_DIR}" >&2
    exit 1
fi
PLAY_REL="playbooks/imports/play-python.yml"

pipx_python=""
before_ref="fadc1eca"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --pipx-python) pipx_python="${2:?--pipx-python needs a value}"; shift 2 ;;
        --before-ref) before_ref="${2:?--before-ref needs a value}"; shift 2 ;;
        -h|--help)
            printf 'usage: %s --pipx-python <python> [--before-ref <git ref>]\n' "$(basename "$0")"
            exit 0
            ;;
        *) printf 'ERROR: unknown argument %s\n' "$1" >&2; exit 2 ;;
    esac
done
if [[ -z "${pipx_python}" ]]; then
    printf 'ERROR: --pipx-python is required\n' >&2
    exit 2
fi
for tool in ansible-playbook ansible-doc python3 git uv "${pipx_python}"; do # STANDARD-EXCEPTION(R6): sealed localhost run, see header
    if ! command -v "${tool}" >/dev/null; then
        printf 'ERROR: %s not found\n' "${tool}" >&2
        exit 2
    fi
done
if ! pipx_version=$("${pipx_python}" -m pipx --version 2>&1); then
    printf 'ERROR: %s -m pipx does not run: %s\n' "${pipx_python}" "${pipx_version}" >&2
    exit 2
fi
uv_bin="$(command -v uv)"
uv_version="$("${uv_bin}" --version)"
echo "pipx ${pipx_version}, ${uv_version}, before = ${before_ref}, after = working tree"

work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT
mkdir -p "${work}/tmp" "${work}/wheels" "${work}/uvbin"
ln -s "${uv_bin}" "${work}/uvbin/uv"
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' \
    > "${work}/ansible.cfg"

# run_ansible <command> <args...> — a sealed ansible run: the caller's ANSIBLE_* settings are
# dropped, bar where collections live.
run_ansible() {
    env -i PATH="${PATH}" HOME="${HOME}" LANG=C.UTF-8 \
        ${ANSIBLE_COLLECTIONS_PATH:+ANSIBLE_COLLECTIONS_PATH="${ANSIBLE_COLLECTIONS_PATH}"} \
        ANSIBLE_CONFIG="${work}/ansible.cfg" \
        ANSIBLE_LOCAL_TEMP="${work}/tmp" ANSIBLE_REMOTE_TEMP="${work}/tmp" \
        "$@" </dev/null 2>&1
}

if ! doc_out=$(run_ansible ansible-doc -t module community.general.pipx); then
    echo "ERROR: community.general is not installed — ansible-galaxy install -r requirements.yml" >&2
    printf '%s\n' "${doc_out}" >&2
    exit 2
fi

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "${got}" = "${want}" ]; then
        passed=$((passed + 1))
        echo "  PASS: ${label}"
    else
        failed=$((failed + 1))
        echo "  FAIL: ${label} → '${got}' (wanted '${want}')"
    fi
}

# Fake ruff and semgrep wheels. Each installs a console script printing "<name> <version>",
# which is all the play's `--version` probe reads.
if ! python3 - "${work}/wheels" ruff:9.0.1 ruff:9.0.2 semgrep:9.0.1 semgrep:9.0.2 <<'PY'
import base64
import hashlib
import pathlib
import sys
import zipfile

out = pathlib.Path(sys.argv[1])
for spec in sys.argv[2:]:
    name, version = spec.split(":")
    module = f"fake_{name}"
    dist = f"{name}-{version}.dist-info"
    files = {
        f"{module}/__init__.py": f"def main():\n    print('{name} {version}')\n",
        f"{dist}/METADATA": f"Metadata-Version: 2.1\nName: {name}\nVersion: {version}\n",
        f"{dist}/WHEEL": "Wheel-Version: 1.0\nGenerator: reproduce-pipx-uv\nRoot-Is-Purelib: true\nTag: py3-none-any\n",
        f"{dist}/entry_points.txt": f"[console_scripts]\n{name} = {module}:main\n",
    }
    record = []
    for path, text in files.items():
        digest = base64.urlsafe_b64encode(hashlib.sha256(text.encode()).digest()).rstrip(b"=").decode()
        record.append(f"{path},sha256={digest},{len(text.encode())}")
    record.append(f"{dist}/RECORD,,")
    files[f"{dist}/RECORD"] = "\n".join(record) + "\n"
    with zipfile.ZipFile(out / f"{name}-{version}-py3-none-any.whl", "w") as wheel:
        for path, text in files.items():
            wheel.writestr(path, text)
PY
then
    echo "ERROR: could not build the fake wheels" >&2
    exit 1
fi

if ! git -C "${REPO_ROOT}" show "${before_ref}:${PLAY_REL}" > "${work}/before-play.yml"; then
    echo "ERROR: cannot read ${PLAY_REL} at ${before_ref}" >&2
    exit 2
fi
cp "${REPO_ROOT}/${PLAY_REL}" "${work}/after-play.yml"
printf 'all:\n  hosts:\n    localhost:\n      ansible_connection: local\n' > "${work}/dump-inventory.yml"

# lift <label> <play file> — writes <label>.yml: the play's tasks from the version probe
# (register: qa_tool_installed) through the pin task, unchanged. Ansible parses the play, as
# it will when it runs it.
lift() {
    local label="$1" play="$2" out
    cat > "${work}/dump-${label}.yml" <<EOF
- hosts: localhost
  gather_facts: false
  tasks:
    - name: Dump the Play as JSON
      ansible.builtin.copy:
        dest: "${work}/${label}.json"
        content: "{{ lookup('ansible.builtin.file', '${play}') | from_yaml | to_json }}"
        mode: "0600"
EOF
    if ! out=$(run_ansible ansible-playbook -i "${work}/dump-inventory.yml" "${work}/dump-${label}.yml"); then # STANDARD-EXCEPTION(R6): sealed localhost run, see header
        echo "ERROR: ansible could not parse ${play}:" >&2
        printf '%s\n' "${out}" >&2
        return 1
    fi
    python3 - "${work}/${label}.json" "${work}/${label}.yml" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    plays = json.load(handle)
tasks = plays[0]["tasks"]
start = next(i for i, t in enumerate(tasks) if t.get("register") == "qa_tool_installed")
end = next(
    i for i, t in enumerate(tasks)
    if i > start and t.get("community.general.pipx", {}).get("state") == "pin"
)
lifted = tasks[start:end + 1]
with open(sys.argv[2], "w", encoding="utf-8") as handle:
    json.dump([{
        "hosts": "desktop",
        "gather_facts": False,
        "environment": "{{ sealed_env }}",
        "tasks": lifted,
    }], handle)
print("  " + " | ".join(t.get("name", "<unnamed>") for t in lifted))
PY
}
echo "=== tasks lifted ==="
lift before "${work}/before-play.yml" || exit 1
lift after "${work}/after-play.yml" || exit 1

# case_env <case> — prints the case's environment, one NAME=value per line.
case_env() {
    local dir="${work}/case-$1"
    printf '%s\n' \
        "HOME=${dir}/home" \
        "PIPX_HOME=${dir}/pipx" \
        "PIPX_BIN_DIR=${dir}/bin" \
        "PIPX_MAN_DIR=${dir}/man" \
        "XDG_CACHE_HOME=${dir}/cache" \
        "XDG_DATA_HOME=${dir}/data" \
        "XDG_STATE_HOME=${dir}/state" \
        "UV_CACHE_DIR=${dir}/uv-cache" \
        "UV_NO_INDEX=1" \
        "UV_FIND_LINKS=${work}/wheels" \
        "UV_PYTHON_DOWNLOADS=never" \
        "PATH=${dir}/bin:${work}/uvbin:/usr/bin:/bin"
}

# pipx_in <case> <pipx args...> — the same pipx the play uses, in the case's environment.
pipx_in() {
    local name="$1"
    shift
    local -a vars
    mapfile -t vars < <(case_env "${name}")
    env -i LANG=C.UTF-8 "${vars[@]}" "${pipx_python}" -m pipx "$@" </dev/null 2>&1
}

# new_case <case> — an empty HOME, pipx home and bin dir, and an inventory pointing the tasks
# at them with the pin at 9.0.2.
new_case() {
    local name="$1" dir="${work}/case-$1" line key value
    mkdir -p "${dir}/home" "${dir}/bin"
    {
        printf 'all:\n  children:\n    desktop:\n      hosts:\n        localhost:\n'
        printf '          ansible_connection: local\n'
        printf '          ansible_python_interpreter: "%s"\n' "${pipx_python}"
        printf '          qa_versions: {RUFF: "9.0.2", SEMGREP: "9.0.2"}\n'
        printf '          sealed_env:\n'
        while IFS= read -r line; do
            key="${line%%=*}"
            value="${line#*=}"
            printf '            %s: "%s"\n' "${key}" "${value}"
        done < <(case_env "${name}")
    } > "${dir}/inventory.yml"
}

# run_tasks <case> <before|after> — runs the lifted tasks; prints ansible's output.
run_tasks() {
    run_ansible ansible-playbook -i "${work}/case-$1/inventory.yml" "${work}/$2.yml" # STANDARD-EXCEPTION(R6): sealed localhost run, see header
}

# tool_says <case> <tool> — what the case's installed tool prints, or `absent`.
tool_says() {
    local bin="${work}/case-$1/bin/$2" out
    if [ ! -x "${bin}" ]; then
        printf 'absent'
        return
    fi
    if out=$("${bin}" 2>&1); then printf '%s' "${out}"; else printf 'broken: %s' "${out}"; fi
}

# venv_meta <case> <tool> <pinned|backend> — from the venv's pipx_metadata.json.
venv_meta() {
    python3 - "${work}/case-$1/pipx/venvs/$2/pipx_metadata.json" "$3" <<'PY'
import json
import pathlib
import sys

meta_file = pathlib.Path(sys.argv[1])
if not meta_file.is_file():
    print("no-venv", end="")
    sys.exit(0)
meta = json.loads(meta_file.read_text(encoding="utf-8"))
if sys.argv[2] == "pinned":
    print(str(meta["main_package"]["pinned"]).lower(), end="")
else:
    print(meta.get("backend", "unrecorded"), end="")
PY
}

# changed_count <ansible output> — the recap's changed= for localhost.
changed_count() {
    awk '/^localhost[[:space:]]+:/ { for (i = 1; i <= NF; i++) if ($i ~ /^changed=/) { sub(/^changed=/, "", $i); print $i } }' <<< "$1"
}

# rerun_changes_nothing <case> <before|after> — runs the tasks again; they must succeed and
# the recap must say changed=0. A failed run reports `run-failed`, never a count.
rerun_changes_nothing() {
    local name="$1" code="$2" out rc
    out=$(run_tasks "${name}" "${code}"); rc=$?
    if [ "${rc}" -ne 0 ]; then
        printf '%s\n' "${out}" >&2
        check "${name}: a second run succeeds and changes nothing" "0" "run-failed"
        return
    fi
    check "${name}: a second run succeeds and changes nothing" "0" "$(changed_count "${out}")"
}

# prestate <case> <pin: yes|no> — semgrep installed off-pin by pipx under uv, ruff at the pin.
prestate() {
    local name="$1" pin="$2" out tool
    if ! out=$(pipx_in "${name}" install "semgrep==9.0.1"); then
        echo "ERROR: pre-state install of semgrep 9.0.1 failed for ${name}:" >&2
        printf '%s\n' "${out}" >&2
        exit 1
    fi
    if ! out=$(pipx_in "${name}" install "ruff==9.0.2"); then
        echo "ERROR: pre-state install of ruff 9.0.2 failed for ${name}:" >&2
        printf '%s\n' "${out}" >&2
        exit 1
    fi
    if [ "${pin}" = "yes" ]; then
        for tool in ruff semgrep; do
            if ! out=$(pipx_in "${name}" pin "${tool}"); then
                echo "ERROR: pre-state pin of ${tool} failed for ${name}:" >&2
                printf '%s\n' "${out}" >&2
                exit 1
            fi
        done
    fi
    check "${name}: pre-state semgrep venv is recorded as uv-backed" "uv" "$(venv_meta "${name}" semgrep backend)"
    check "${name}: pre-state semgrep is 9.0.1" "semgrep 9.0.1" "$(tool_says "${name}" semgrep)"
    check "${name}: pre-state semgrep pinned is ${pin}" "$([ "${pin}" = yes ] && echo true || echo false)" \
        "$(venv_meta "${name}" semgrep pinned)"
}

UV_EXISTS='A virtual environment already exists'

for code in before after; do
    echo "=== ${code}: nothing installed ==="
    name="${code}-fresh"
    new_case "${name}"
    out=$(run_tasks "${name}" "${code}"); rc=$?
    check "${name}: the tasks succeed" "0" "${rc}"
    [ "${rc}" -eq 0 ] || printf '%s\n' "${out}" >&2
    check "${name}: ruff is at the pin" "ruff 9.0.2" "$(tool_says "${name}" ruff)"
    check "${name}: semgrep is at the pin" "semgrep 9.0.2" "$(tool_says "${name}" semgrep)"
    check "${name}: semgrep is pinned" "true" "$(venv_meta "${name}" semgrep pinned)"
    rerun_changes_nothing "${name}" "${code}"

    for pin in no yes; do
        label=$([ "${pin}" = yes ] && echo "pinned" || echo "unpinned")
        echo "=== ${code}: semgrep at 9.0.1, ${label}, uv-backed; the pin is 9.0.2 ==="
        name="${code}-mismatch-${label}"
        new_case "${name}"
        prestate "${name}" "${pin}"
        out=$(run_tasks "${name}" "${code}"); rc=$?
        said=0
        if grep -q "${UV_EXISTS}" <<< "${out}"; then said=1; fi
        if [ "${code}" = before ]; then
            check "${name}: the tasks FAIL" "failed" "$([ "${rc}" -ne 0 ] && echo failed || echo passed)"
            check "${name}: uv refuses the existing venv ('${UV_EXISTS}')" "1" "${said}"
            if [ "${said}" -eq 1 ]; then
                echo "  --- what ansible reported ---"
                grep -E -o '"cmd": "[^"]*"|A virtual environment already exists[^"\\]*|Not removing existing venv[^"\\]*' <<< "${out}" \
                    | awk '!seen[$0]++ { print "  | " $0 }'
            fi
            check "${name}: semgrep is left at 9.0.1" "semgrep 9.0.1" "$(tool_says "${name}" semgrep)"
        else
            check "${name}: the tasks succeed" "0" "${rc}"
            [ "${rc}" -eq 0 ] || printf '%s\n' "${out}" >&2
            check "${name}: uv never refused a venv" "0" "${said}"
            check "${name}: semgrep is now at the pin" "semgrep 9.0.2" "$(tool_says "${name}" semgrep)"
            check "${name}: semgrep is pinned" "true" "$(venv_meta "${name}" semgrep pinned)"
            check "${name}: ruff, already at the pin, still is" "ruff 9.0.2" "$(tool_says "${name}" ruff)"
            check "${name}: ruff is pinned" "true" "$(venv_meta "${name}" ruff pinned)"
            echo "  (first run: recap changed=$(changed_count "${out}"))"
            rerun_changes_nothing "${name}" "${code}"
        fi
    done
done

echo
echo "passed: ${passed}  failed: ${failed}"
[ "${failed}" -eq 0 ]
