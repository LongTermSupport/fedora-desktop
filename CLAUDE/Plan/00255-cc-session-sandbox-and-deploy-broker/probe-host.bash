#!/usr/bin/env bash
# probe-host.bash — Plan 00255 Phase 0: the host facts behind a sealed cc session and a deploy
# broker.
#
# Called by triage.bash, one leg per mode; runnable on its own on the HOST for one mode.
# Appends what it finds to REPORT. A probe that exits non-zero is recorded as data with its exit
# code; the mode itself fails only when it cannot run at all. Renders no verdict
# (PlanScriptStandards.md R9).
#
#   tools    bwrap, xdg-dbus-proxy (and its filter options: is there any deny form?), systemd-run
#   kernel   user.max_user_namespaces, kernel.yama.ptrace_scope
#   paths    which folders the seal would hide exist here: existence and type ONLY, never a
#            listing or a read of what is inside. Matrix clients come from clients.json.
#   bus      well-known names on the session bus; where gh and git keep their logins
#   sudo     `sudo -n -l` (rules and Defaults, never prompts) and the sudo version
#   escape   the sockets a sealed session could use to start a process outside the seal (tmux,
#            the systemd user manager) and the self-update runner's clone and config folder
#   active   two bwrap runs: `sudo -n true` inside a user namespace, and bwrap inside bwrap
#
# EFFECT ON THE HOST: none. `active` starts bwrap twice, each running one command and exiting.
# SECRETS: no file under a hidden folder is opened or listed; gh output keeps only the lines that
# name its store, with anything token-shaped substituted.
#
# Usage: probe-host.bash tools|kernel|paths|bus|sudo|escape|active REPORT CLIENTS_JSON
set -euo pipefail

readonly USAGE="usage: probe-host.bash tools|kernel|paths|bus|sudo|escape|active REPORT CLIENTS_JSON"
mode="${1:?${USAGE}}"
report="${2:?${USAGE}}"
clients="${3:?${USAGE}}"

# probe <label> <cmd...> — a non-zero exit is data: record it and carry on.
probe() {
  local label="$1" out rc
  shift
  if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
  printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}" >>"${report}"
}

# kind <path> — "absent", or the file type and mode. Never lists or reads the path.
kind() {
  if [[ -e "$1" || -L "$1" ]]; then stat -c '%F %a' -- "$1"; else printf 'absent\n'; fi
}

# candidates — the folders the seal would hide, one per line.
candidates() {
  jq -r --arg h "${HOME}" '.clients[] | ($h + "/.var/app/" + .app_id), (.native[] | $h + "/" + .parent + "/" + .name)' "${clients}"
  printf "${HOME}/%s\n" .config/nheko .local/share/nheko .local/share/keyrings .password-store .gnupg .ssh .mozilla \
    .var/app/org.mozilla.firefox .config/google-chrome .var/app/com.google.Chrome \
    .config/chromium .var/app/org.chromium.Chromium .config/BraveSoftware \
    .var/app/com.brave.Browser .config/microsoft-edge .config/vivaldi
}

# ghStore — which store gh keeps its login in. A logged-out gh exits non-zero; that is data.
ghStore() {
  local out rc
  if out="$(gh auth status 2>&1)"; then rc=0; else rc=$?; fi
  printf 'gh auth status exit: %d\n' "${rc}"
  printf '%s\n' "${out}" | awk '/Logged in|not logged|keyring|hosts.yml/ { gsub(/(gh[opsu]|github_pat)_[A-Za-z0-9_]+/, "<redacted>"); print }'
}

mkdir -p "$(dirname "${report}")"
run="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
case "${mode}" in
  tools)
    printf '## tools\n\n' >>"${report}"
    probe "bwrap path" command -v bwrap
    probe "bwrap version" bwrap --version
    probe "xdg-dbus-proxy options (look for a deny form; --see/--talk/--own/--call allow)" xdg-dbus-proxy --help
    probe "systemd-run version" systemd-run --version
    probe "rpm owners" rpm -q bubblewrap xdg-dbus-proxy
    ;;
  kernel)
    printf '## kernel\n\n' >>"${report}"
    probe "user.max_user_namespaces (0 = no unprivileged sandbox)" cat /proc/sys/user/max_user_namespaces
    probe "kernel.yama.ptrace_scope (0 = a same-user process may read another's memory)" cat /proc/sys/kernel/yama/ptrace_scope
    ;;
  paths)
    printf '## folders the seal would hide (existence and type only)\n\n' >>"${report}"
    while IFS= read -r p; do
      printf '%-62s %s\n' "~${p#"${HOME}"}" "$(kind "${p}")" >>"${report}"
    done < <(candidates)
    printf '\n' >>"${report}"
    probe "native Element profiles (folder names only)" find "${HOME}/.config" -maxdepth 1 -name 'Element*' -printf '%f\n'
    ;;
  bus)
    printf '## session bus and credential stores\n\n' >>"${report}"
    probe "session bus names (the proxy's allowlist is chosen from these)" busctl --user --no-pager --no-legend --acquired list
    probe "gh login store (keyring = gh needs the secrets service)" ghStore
    probe "git credential helpers (global)" git config --global --get-all credential.helper
    ;;
  sudo)
    printf '## sudo now\n\n' >>"${report}"
    probe "sudo -n -l (rules and Defaults for this user; never prompts)" sudo -n -l
    probe "sudo version (scoped rules with regular expressions need 1.9.10+)" sudo -V
    ;;
  escape)
    printf '## ways out of a seal, and the self-update runner\n\n' >>"${report}"
    {
      printf 'tmux dir:           %s\n' "$(kind "/tmp/tmux-$(id -u)")"
      printf 'user bus:           %s\n' "$(kind "${run}/bus")"
      printf 'systemd private:    %s\n' "$(kind "${run}/systemd/private")"
      printf 'self-update clone:  %s\n' "$(kind /var/lib/fedora-desktop/deploy)"
      printf 'self-update config: %s\n\n' "$(kind /etc/fedora-desktop)"
    } >>"${report}"
    ;;
  active)
    printf '## active: bwrap runs\n\n' >>"${report}"
    probe "sudo -n true inside a user namespace (expected to fail)" \
      bwrap --ro-bind / / --dev /dev --proc /proc --unshare-user --unshare-pid -- sudo -n true
    probe "bwrap inside bwrap (Claude Code's Bash sandbox would nest)" \
      bwrap --ro-bind / / --dev /dev --proc /proc --unshare-user -- \
      bwrap --ro-bind / / --dev /dev --proc /proc --unshare-user -- true
    ;;
  *)
    printf 'probe-host.bash: unknown mode %s\n%s\n' "${mode}" "${USAGE}" >&2
    exit 2
    ;;
esac
