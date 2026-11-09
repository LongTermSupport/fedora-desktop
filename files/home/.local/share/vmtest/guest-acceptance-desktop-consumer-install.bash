#!/usr/bin/bash
# guest-acceptance-desktop-consumer-install.bash — the assertion set for the
# desktop-consumer-install scenario (Plan 00228), run INSIDE the guest after the host has
# driven `./run.bash --consumer` at a terminal, run it a second time as an update, and
# rebooted. Same contract as guest-acceptance-desktop.bash:
#
#   VMTEST-CHECK-PLANNED <n>                declared BEFORE the first check
#   VMTEST-CHECK pass|fail|skip <name> [detail]
#   VMTEST-EVIDENCE <key>=<value>
#   VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N
#
# What a consumer install promises: no GitHub identity of any kind, the checkout fetched
# and kept over HTTPS, no passwordless sudo from this repo, the everyday apps present, Go
# present (the installer's own build toolchain), speech-to-text (the optional app the
# scenario picks) set up for plain dictation with no Claude post-processing, the developer
# tools absent, and a second run that changes nothing. The second run's output is /var/lib/vmtest/rerun.log, which the
# host's run tees there.
#
# Inputs (environment, set by the host over SSH):
#   VMTEST_COMMIT       the 40-hex commit the guest was told to provision from
set -uo pipefail

PLANNED=20
readonly PLANNED
REPO="${HOME}/Projects/fedora-desktop"
readonly REPO
RERUN_LOG=/var/lib/vmtest/rerun.log
readonly RERUN_LOG

total=0
passed=0
failed=0
skipped=0

check() {
    local status="${1:?}" name="${2:?}" detail="${3:-}"
    total=$((total + 1))
    case "${status}" in
        pass) passed=$((passed + 1)) ;;
        fail) failed=$((failed + 1)) ;;
        skip) skipped=$((skipped + 1)) ;;
        *)
            echo "ERROR: check status must be pass|fail|skip, got ${status}" >&2
            exit 70
            ;;
    esac
    detail="$(printf '%s' "${detail}" | tr '\n' ' ')"
    if [[ -n "${detail}" ]]; then
        printf 'VMTEST-CHECK %s %s %s\n' "${status}" "${name}" "${detail}"
    else
        printf 'VMTEST-CHECK %s %s\n' "${status}" "${name}"
    fi
}

evidence() {
    printf 'VMTEST-EVIDENCE %s=%s\n' "${1:?}" "$(printf '%s' "${2-}" | tr '\n' ' ')"
}

# absent <name> <what> <command...> — pass when the probe command finds nothing (non-zero);
# what the probe printed when it did find something is the failure's detail.
absent() {
    local name="${1:?}" what="${2:?}"
    shift 2
    local found=""
    if found="$("$@" 2>&1)"; then
        check fail "${name}" "${what} is present: ${found}"
    else
        check pass "${name}"
    fi
}

printf 'VMTEST-CHECK-PLANNED %d\n' "${PLANNED}"

# ── the checkout ────────────────────────────────────────────────────────────────────
head=""
if head="$(git -C "${REPO}" rev-parse HEAD 2>&1)"; then
    if [[ "${head}" == "${VMTEST_COMMIT:-}" ]]; then
        check pass repo-cloned-at-pinned-commit "${head:0:12}"
    else
        check fail repo-cloned-at-pinned-commit "HEAD ${head:0:12} is not the pinned ${VMTEST_COMMIT:-unset}"
    fi
else
    check fail repo-cloned-at-pinned-commit "no git checkout at ${REPO}: ${head}"
fi

repo_version="$(grep -E '^fedora_version:' "${REPO}/vars/fedora-version.yml" 2>&1 | awk '{print $2}')"
guest_version="$(. /etc/os-release && printf '%s' "${VERSION_ID}")"
if [[ -n "${repo_version}" && "${repo_version}" == "${guest_version}" ]]; then
    check pass fedora-version-matches-guest "${guest_version}"
else
    check fail fedora-version-matches-guest "repo '${repo_version}' vs guest '${guest_version}'"
fi

# Neither run may have moved origin to SSH, as run.bash's developer path does.
origin="$(git -C "${REPO}" remote get-url origin 2>&1)"
if [[ "${origin}" == https://* ]]; then
    check pass origin-is-https "${origin}"
else
    check fail origin-is-https "origin is '${origin}'"
fi

# ── no GitHub identity ──────────────────────────────────────────────────────────────
host_vars="${REPO}/environment/localhost/host_vars/localhost.yml"
audience_line="$(grep -cx 'install_audience: consumer' "${host_vars}" 2>&1)"
accounts_line="$(grep -cx 'github_accounts: {}' "${host_vars}" 2>&1)"
if [[ "${audience_line}" == 1 && "${accounts_line}" == 1 ]]; then
    check pass localhost-yml-consumer
else
    check fail localhost-yml-consumer "install_audience: consumer lines '${audience_line}', github_accounts: {} lines '${accounts_line}' in ${host_vars}"
fi

# Every key pair run.bash or ssh-keygen makes leaves a .pub beside it; the developer path's
# login key is ~/.ssh/id itself.
keys="$(find "${HOME}/.ssh" -maxdepth 1 -name '*.pub' -print 2>&1)"
if [[ -e "${HOME}/.ssh/id" || -n "${keys}" ]]; then
    check fail no-ssh-key "a key pair under ~/.ssh: ${keys}"
else
    check pass no-ssh-key
fi
absent gh-not-authenticated "gh's login state" ls "${HOME}/.config/gh/hosts.yml"

# ── sudo: this repo's NOPASSWD block is a developer desktop's only (Decision 8) ─────
# The lab user's own NOPASSWD drop-in is the harness's (evidence below), not the play's.
sudoers=""
sudoers_rc=0
sudoers="$(sudo -n grep -F "passwordless sudo for ${USER}" /etc/sudoers 2>&1)" || sudoers_rc=$?
case "${sudoers_rc}" in
    1) check pass sudoers-no-nopasswd-block ;;
    0) check fail sudoers-no-nopasswd-block "/etc/sudoers holds: ${sudoers}" ;;
    *) check fail sudoers-no-nopasswd-block "could not read /etc/sudoers (exit ${sudoers_rc}): ${sudoers}" ;;
esac

# ── what a consumer gets ────────────────────────────────────────────────────────────
apps=""
if apps="$(rpm -q firefox google-chrome-stable rpmfusion-free-release rpmfusion-nonfree-release 2>&1)"; then
    check pass consumer-apps-present "$(printf '%s' "${apps}" | tr '\n' ',')"
else
    check fail consumer-apps-present "${apps}"
fi

go_version=""
if go_version="$(go version 2>&1)"; then
    check pass go-present "${go_version}"
else
    check fail go-present "${go_version}"
fi

if [[ -e "${HOME}/.local/state/fedora-desktop-setup-complete" ]]; then
    check pass setup-complete-marker
else
    check fail setup-complete-marker "${HOME}/.local/state/fedora-desktop-setup-complete is missing"
fi

# ── speech-to-text, the optional app the scenario picks (optional_plays) ──────────────
# Plain dictation is set up as play-speech-to-text.yml sets it up: the panel extension
# loaded by the session the reboot started, Whisper importable by the wrapper that runs it,
# and the commands the panel starts. Claude post-processing is absent: the play compiles
# post-processing-offered false into the extension's own schema on a consumer install, and
# the panel hides its items when it is false.
stt_uuid=speech-to-text@fedora-desktop
stt_schemas="${HOME}/.local/share/gnome-shell/extensions/${stt_uuid}/schemas"
stt_info=""
stt_state=""
if stt_info="$(gnome-extensions info "${stt_uuid}" 2>&1)"; then
    stt_state="$(printf '%s\n' "${stt_info}" | grep -E '^\s*State:' | awk '{print $2}')"
fi
stt_enabled="$(gnome-extensions list --enabled 2>&1 | grep -cx "${stt_uuid}")"
if [[ "${stt_state}" == ACTIVE && "${stt_enabled}" == 1 ]]; then
    check pass stt-extension-active
else
    check fail stt-extension-active "state '${stt_state}', enabled lines '${stt_enabled}': ${stt_info}"
fi

whisper=""
missing_cmds=""
for cmd in faster-whisper-transcribe wsi stt wsi-setting; do
    [[ -x "${HOME}/.local/bin/${cmd}" ]] || missing_cmds="${missing_cmds} ${cmd}"
done
# The wrapper runs this with python3, so it is installed readable, not executable.
[[ -r "${HOME}/.local/bin/faster-whisper-transcribe.py" ]] || missing_cmds="${missing_cmds} faster-whisper-transcribe.py"
if ! whisper="$(python3 -c 'import faster_whisper; print(faster_whisper.__version__)' 2>&1)"; then
    check fail stt-whisper-set-up "faster_whisper does not import: ${whisper}"
elif [[ -n "${missing_cmds}" ]]; then
    check fail stt-whisper-set-up "missing or not usable in ~/.local/bin:${missing_cmds}"
else
    check pass stt-whisper-set-up "faster-whisper ${whisper}"
fi

offered=""
if offered="$(gsettings --schemadir "${stt_schemas}" get org.gnome.shell.extensions.speech-to-text post-processing-offered 2>&1)" \
    && [[ "${offered}" == false ]]; then
    check pass stt-post-processing-not-offered
else
    check fail stt-post-processing-not-offered "post-processing-offered is '${offered}' in ${stt_schemas}"
fi

# ── what a consumer does not get ────────────────────────────────────────────────────
absent node-absent "Node (nvm)" ls -d "${HOME}/.nvm"
absent claude-code-absent "Claude Code" bash -c 'command -v claude'
absent ccy-absent "the ccy launcher" ls /var/local/claude-yolo/claude-yolo
absent vscode-absent "VS Code" rpm -q code
absent lxc-absent "LXC" rpm -q lxc
# Podman itself is on a consumer box (Fedora ships it, and ccc runs in it), so what is judged
# is the podman play's developer part: podman-compose (a pip --user install, or the rpm) and
# the user's podman.socket.
compose_rc=0
compose="$(rpm -q podman-compose 2>&1)" || compose_rc=$?
podman_socket="$(systemctl --user is-enabled podman.socket 2>&1)"
if [[ "${compose_rc}" -eq 0 || -e "${HOME}/.local/bin/podman-compose" || "${podman_socket}" == enabled ]]; then
    check fail podman-developer-part-absent "podman-compose: ${compose}; podman.socket: ${podman_socket}"
else
    check pass podman-developer-part-absent "podman.socket: ${podman_socket}"
fi

# ── the second run changed nothing ──────────────────────────────────────────────────
recaps=""
if [[ -r "${RERUN_LOG}" ]]; then
    recaps="$(grep -E '^[^ ].* : ok=[0-9]+ +changed=[0-9]+' "${RERUN_LOG}")"
fi
not_clean=""
while IFS= read -r recap; do
    if [[ -n "${recap}" && ! ("${recap}" =~ \ changed=0\  && "${recap}" =~ \ failed=0\ ) ]]; then
        not_clean="${not_clean}${recap}; "
    fi
done <<<"${recaps}"
if [[ -z "${recaps}" ]]; then
    check fail rerun-changed-nothing "no PLAY RECAP line in ${RERUN_LOG}"
elif [[ -n "${not_clean}" ]]; then
    check fail rerun-changed-nothing "a recap with changes or failures: ${not_clean}"
else
    check pass rerun-changed-nothing "$(printf '%s\n' "${recaps}" | grep -c .) recap line(s), all changed=0 failed=0"
fi

# ── evidence (never a check) ────────────────────────────────────────────────────────
evidence boot_id "$(cat /proc/sys/kernel/random/boot_id)"
evidence machine_id "$(cat /etc/machine-id)"
evidence os_release "$(. /etc/os-release && printf '%s' "${PRETTY_NAME}")"
evidence kernel "$(uname -r)"
evidence repo_commit "${head}"
evidence repo_branch "$(git -C "${REPO}" rev-parse --abbrev-ref HEAD 2>&1)"
evidence lab_sudoers_dropin "$(sudo -n cat /etc/sudoers.d/90-vmtest-lab 2>&1)"
evidence rerun_recaps "${recaps}"
repoinfo=""
if repoinfo="$(sudo -n dnf --quiet repoinfo updates 2>&1)"; then
    evidence updates_revision "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Revision\s*:' | awk -F: '{gsub(/ /, "", $2); print $2}')"
    evidence updates_mirror "$(printf '%s\n' "${repoinfo}" | grep -E '^\s*Base URL\s*:' | awk '{print $4}')"
else
    evidence updates_revision ""
    evidence updates_mirror ""
    echo "WARNING: dnf repoinfo updates failed: ${repoinfo}" >&2
fi

printf 'VMTEST-CHECKS-DONE total=%d passed=%d failed=%d skipped=%d\n' "${total}" "${passed}" "${failed}" "${skipped}"
