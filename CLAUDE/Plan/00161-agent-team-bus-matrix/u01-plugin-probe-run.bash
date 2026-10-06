#!/usr/bin/env bash
# u01-plugin-probe-run.bash — one U01 run: declare three marker hooks one way, run a child
# `claude -p`, collect the evidence and hand it to u01-plugin-probe-check.bash
# (DESIGN.md sections 6 and 13).
#
# The hooks: SessionStart, UserPromptSubmit and Stop each touch <evidence-dir>/marks/<Event>.
# Install ways:
#   plugin         the route under test, the phpantom-lsp way: the plugin is copied to
#                  <config>/plugins/u01probe and enabled with a bare enabledPlugins key
#   user-settings  control, and the D13 fallback: the same hooks in <config>/settings.json
#   plugin-dir     control: the same plugin loaded with --plugin-dir, showing the plugin
#                  itself is well formed whatever the plugin route does
# Every file lives under <evidence-dir>. The child gets CLAUDE_CONFIG_DIR=<evidence-dir>/config:
# the lookup the entrypoint relies on for /root/.claude, without touching the live
# /root/.claude, which the running session shares through the /workspace/.claude/ccy symlink.
#
# Working directory:
#   neutral  an empty directory, so only the probe hooks are in play
#   project  the repo root, so the project's hooks daemon (and its
#            hook_registration_checker) also runs; its debug lines are kept for review
#
# Credential: `ccy-claude` (CCY_CHILD_CLAUDE mode) when it is on PATH, otherwise plain
# `claude`, which has no credential in a scratch config directory. The launcher is recorded.
#
# stdout: the checker's lines (also appended to <report-file>). Exit: the checker's.
# A control is checked with --hooks-only; the plugin run is checked against
# <control-evidence-dir> when one is given.
#
# Usage: u01-plugin-probe-run.bash <evidence-dir> <plugin|user-settings|plugin-dir>
#            <neutral|project> <report-file> [<control-evidence-dir>]
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
CHECKER="${HERE}/u01-plugin-probe-check.bash"
readonly PLUGIN_NAME="u01probe"
readonly CHILD_TIMEOUT_SECONDS=300
readonly PROMPT="Reply with the single word OK and nothing else."

usage() {
    printf 'usage: u01-plugin-probe-run.bash <evidence-dir> <plugin|user-settings|plugin-dir> <neutral|project> <report-file> [<control-evidence-dir>]\n' >&2
    exit 64
}

[[ "$#" -ge 4 ]] && [[ "$#" -le 5 ]] || usage
evidence="$1"
install="$2"
where="$3"
report="$4"
control="${5:-}"

repoRoot="${HERE}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/.git" ]]; do
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/.git" ]] || { printf '[FATAL] no repo root above %s\n' "${HERE}" >&2; exit 1; }

case "${install}" in
    plugin | user-settings | plugin-dir) ;;
    *) usage ;;
esac
case "${where}" in
    neutral) childCwd="${evidence}/cwd" ;;
    project) childCwd="${repoRoot}" ;;
    *) usage ;;
esac
if [[ -e "${evidence}" ]]; then
    printf 'u01-plugin-probe-run: %s already exists; each run needs a fresh directory\n' "${evidence}" >&2
    exit 73
fi
for tool in jq claude timeout; do
    command -v "${tool}" >/dev/null || { printf 'u01-plugin-probe-run: %s is not on PATH\n' "${tool}" >&2; exit 69; }
done

config="${evidence}/config"
marks="${evidence}/marks"
mkdir -p "${config}" "${marks}" "${evidence}/cwd"

# The three hooks, as the "hooks" object both plugin hooks.json and settings.json use. The
# marker path is quoted with jq's @sh so a space in the checkout path cannot split it.
hooksObject="$(jq -n --arg marks "${marks}" '
    def touch(event): [{hooks: [{type: "command", command: ("touch " + ($marks + "/" + event | @sh))}]}];
    {SessionStart: touch("SessionStart"),
     UserPromptSubmit: touch("UserPromptSubmit"),
     Stop: touch("Stop")}')"

# build_plugin <dir> — write the throwaway plugin into <dir>.
build_plugin() {
    local dir="$1"
    mkdir -p "${dir}/.claude-plugin" "${dir}/hooks"
    jq -n --arg name "${PLUGIN_NAME}" \
        '{name: $name, version: "0.0.1", description: "Throwaway Plan 00161 U01 hook-loading probe"}' \
        >"${dir}/.claude-plugin/plugin.json"
    jq -n --argjson hooks "${hooksObject}" '{hooks: $hooks}' >"${dir}/hooks/hooks.json"
}

extraArgs=()
case "${install}" in
    plugin)
        build_plugin "${config}/plugins/${PLUGIN_NAME}"
        jq -n --arg name "${PLUGIN_NAME}" '{enabledPlugins: {($name): true}}' >"${config}/settings.json"
        ;;
    user-settings)
        jq -n --argjson hooks "${hooksObject}" '{hooks: $hooks}' >"${config}/settings.json"
        ;;
    plugin-dir)
        build_plugin "${evidence}/plugin-src/${PLUGIN_NAME}"
        jq -n '{}' >"${config}/settings.json"
        extraArgs=(--plugin-dir "${evidence}/plugin-src/${PLUGIN_NAME}")
        ;;
esac

if command -v ccy-claude >/dev/null; then
    launcher=ccy-claude
else
    launcher=claude
fi
printf '%s\n' "${launcher}" >"${evidence}/launcher"
claude --version >"${evidence}/claude-version"

# What the CLI itself lists, before any session. Recorded with its exit code; a refusal
# here is evidence, not a reason to stop.
listRc=0
(cd "${childCwd}" && CLAUDE_CONFIG_DIR="${config}" claude plugin list </dev/null) \
    >"${evidence}/plugin-list.stdout" 2>"${evidence}/plugin-list.stderr" || listRc=$?
printf '%s\n' "${listRc}" >"${evidence}/plugin-list.rc"
listed=no
if grep -q -F "${PLUGIN_NAME}" "${evidence}/plugin-list.stdout"; then
    listed=yes
fi

childRc=0
(cd "${childCwd}" && CLAUDE_CONFIG_DIR="${config}" timeout "${CHILD_TIMEOUT_SECONDS}" \
    "${launcher}" -p "${PROMPT}" --model haiku --debug-file "${evidence}/debug.log" \
    "${extraArgs[@]}" </dev/null) \
    >"${evidence}/child.stdout" 2>"${evidence}/child.stderr" || childRc=$?
printf '%s\n' "${childRc}" >"${evidence}/child.rc"

# What the child's own debug log says it registered: "Registered <n> hooks from <m> plugins".
registered="$(awk '/Registered [0-9]+ hooks from [0-9]+ plugins/ { line = $0 } END { sub(/.*Registered /, "", line); print line }' \
    "${evidence}/debug.log")"

# The project variant asks whether the hooks daemon's hook_registration_checker says
# anything. Keep every debug line naming it or its findings; none is a valid answer.
# A checkout with no provisioned daemon (a fresh worktree) answers every hook with the
# NEEDS PROVISIONING notice, so its checker cannot have run; say so rather than report 0.
checkerLines=n/a
daemonState=n/a
if [[ "${where}" == project ]]; then
    if grep -q -F 'NEEDS PROVISIONING' "${evidence}/debug.log"; then
        daemonState=not-provisioned
    elif grep -q -F '.claude/hooks/session-start' "${evidence}/debug.log"; then
        daemonState=ran
    else
        daemonState=absent
    fi
    matchRc=0
    grep -n -i -E 'hook_registration|hook registration|registered in BOTH' \
        "${evidence}/debug.log" >"${evidence}/hook-registration-checker.txt" || matchRc=$?
    if [[ "${matchRc}" -gt 1 ]]; then
        printf 'u01-plugin-probe-run: grep failed (%s) on %s/debug.log\n' "${matchRc}" "${evidence}" >&2
        exit 1
    fi
    checkerLines="$(wc -l <"${evidence}/hook-registration-checker.txt")"
fi

checkArgs=("${evidence}")
if [[ "${install}" != plugin ]]; then
    checkArgs+=(--hooks-only)
elif [[ -n "${control}" ]]; then
    checkArgs+=("${control}")
fi
checkRc=0
"${CHECKER}" "${checkArgs[@]}" >"${evidence}/check.txt" || checkRc=$?
{
    printf '# U01 run: install=%s cwd=%s\n' "${install}" "${where}"
    printf 'launcher %s\n' "${launcher}"
    printf 'claude-version %s\n' "$(<"${evidence}/claude-version")"
    printf 'plugin-list rc=%s lists-%s=%s\n' "${listRc}" "${PLUGIN_NAME}" "${listed}"
    printf 'debug-log registered %s\n' "${registered:-none}"
    printf 'hooks-daemon %s hook-registration-checker debug-lines=%s\n' "${daemonState}" "${checkerLines}"
    cat "${evidence}/check.txt"
} | tee -a "${report}"
exit "${checkRc}"
