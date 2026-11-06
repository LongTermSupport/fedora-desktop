#!/usr/bin/env bash
# probe-storage.bash — one leg of Plan 00249's triage.bash: measure homeserver storage.
#
#   probe-storage.bash v1|v2|df REPORT
#
# v1   every team under /var/lib/agent-bus that has a team.json (agent-bus), with its state
#      tars under /var/lib/agent-bus-install/<team>/backups;
# v2   every team under /var/lib/private/teamchat (teamchat), with its tars under
#      /var/lib/teamchat-admin/<team>/backups;
# df   free space and inodes of the filesystems those roots are on.
#
# Appends to REPORT. Read-only: du, find, stat and df through `sudo -n` (the caller primed
# sudo). Sizes are bytes (du -sb, apparent size) and blocks on disk (du -sk), because RocksDB
# preallocates and a backup's shared files are hard links. A probe that exits non-zero is
# recorded with its exit code and makes this leg exit 1, so the gap is never silent.
set -euo pipefail

readonly MODE="${1:?usage: probe-storage.bash v1|v2|df REPORT}"
readonly REPORT="${2:?usage: probe-storage.bash v1|v2|df REPORT}"
# A test runs this against a fake tree; triage.bash never sets it.
readonly PREFIX="${PROBE_STORAGE_PREFIX:-}"
# agent-bus's state roots, handed over by triage.bash, which reads them from the product
# (render.py STATE_ROOT, health.py INSTALL_STATE_ROOT) so this script holds no copy of them.
readonly V1_ROOT="${PREFIX}${AGENT_BUS_STATE_ROOT:?set by triage.bash from render.py STATE_ROOT}"
readonly V1_TARS="${PREFIX}${AGENT_BUS_INSTALL_ROOT:?set by triage.bash from health.py INSTALL_STATE_ROOT}"
FAILED=0

probe() {
  local label="$1" out rc
  shift
  if out="$(sudo -n "$@" 2>&1)"; then rc=0; else rc=$?; FAILED=1; fi
  printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${out:-(no output)}" >>"${REPORT}"
}

# Count and total the files under a directory by extension, to the given depth.
by_kind() {
  local label="$1" dir="$2" depth="$3" out rc=0 summary
  if ! out="$(sudo -n find "${dir}" -maxdepth "${depth}" -type f -printf '%f %s\n' 2>&1)"; then
    rc=$?
    FAILED=1
    summary="${out}"
  else
    summary="$(awk '{ k = $1; if (k ~ /\./) sub(/.*\./, "", k); else k = "(none)"; c[k]++; s[k] += $2 }
      END { for (k in c) printf "%-10s %8d files %16d bytes\n", k, c[k], s[k] }' <<<"${out}")"
  fi
  printf '### %s  (rc=%d)\n%s\n\n' "${label}" "${rc}" "${summary:-(no files)}" >>"${REPORT}"
}

# One team's homeserver tree: each top-level entry's size (secrets/ by size only, never
# read), the database's SST and WAL files, Tuwunel's backup engine directories, media.
measure_team() {
  local team="$1" dir="$2" tars="$3"
  printf '## team %s (%s)\n\n' "${team}" "${MODE}" >>"${REPORT}"
  probe "${team}: each entry of the team directory, bytes" du -sb --apparent-size "${dir}"/*
  probe "${team}: each entry of the team directory, KiB on disk" du -sk "${dir}"/*
  by_kind "${team}: database files by extension (sst, log = WAL, ...)" "${dir}/db" 1
  if sudo -n test -d "${dir}/db/archive"; then
    probe "${team}: RocksDB WAL archive (db/archive), total" du -sb --apparent-size "${dir}/db/archive"
    by_kind "${team}: RocksDB WAL archive files by extension" "${dir}/db/archive" 1
    probe "${team}: WAL archive, each file (mtime, bytes, name)" \
      find "${dir}/db/archive" -maxdepth 1 -type f -printf '%TY-%Tm-%Td %s %f\n'
  fi
  probe "${team}: live WAL and MANIFEST (their mtime is the database's last write)" \
    find "${dir}/db" -maxdepth 1 -type f '(' -name '*.log' -o -name 'MANIFEST-*' ')' -printf '%TY-%Tm-%Td %TH:%TM %s %f\n'
  probe "${team}: database backups (Tuwunel backup engine), per subdirectory" \
    du -sb --apparent-size "${dir}/backups"/*
  probe "${team}: database backup numbers and times (meta/)" \
    find "${dir}/backups/meta" -maxdepth 1 -type f -printf '%f %TY-%Tm-%Td %s\n'
  by_kind "${team}: backup engine files by extension" "${dir}/backups" 3
  if sudo -n test -d "${dir}/db/media"; then
    probe "${team}: media under the database directory" du -sb --apparent-size "${dir}/db/media"
  fi
  if sudo -n test -d "${dir}/media"; then
    probe "${team}: media beside the database" du -sb --apparent-size "${dir}/media"
  fi
  if sudo -n test -d "${tars}"; then
    probe "${team}: installer state tars, total" du -sb --apparent-size "${tars}"
    probe "${team}: installer state tars, each" find "${tars}" -maxdepth 1 -type f -printf '%f %s %TY-%Tm-%Td\n'
  else
    printf '### %s: no state tar directory at %s\n\n' "${team}" "${tars}" >>"${REPORT}"
  fi
}

case "${MODE}" in
  v1 | v2)
    if [[ "${MODE}" == v1 ]]; then
      root=${V1_ROOT} tarRoot=${V1_TARS} marker=team.json
    else
      root=${PREFIX}/var/lib/private/teamchat tarRoot=${PREFIX}/var/lib/teamchat-admin marker=db
    fi
    if ! sudo -n test -d "${root}"; then
      printf '## %s: %s is absent (not installed here)\n\n' "${MODE}" "${root}" >>"${REPORT}"
      exit 0
    fi
    probe "${MODE}: the whole state root" du -sb --apparent-size "${root}"
    teams="$(sudo -n find "${root}" -mindepth 2 -maxdepth 2 -name "${marker}" -printf '%h\n')"
    if [[ -z "${teams}" ]]; then
      printf '## %s: no team under %s\n\n' "${MODE}" "${root}" >>"${REPORT}"
    fi
    while IFS= read -r teamDir; do
      [[ -n "${teamDir}" ]] || continue
      team="$(basename "${teamDir}")"
      measure_team "${team}" "${teamDir}" "${tarRoot}/${team}/backups"
    done <<<"${teams}"
    ;;
  df)
    for path in "${PREFIX}/var/lib" "${V1_ROOT}" "${PREFIX}/var/lib/private" "${V1_TARS}"; do
      if sudo -n test -e "${path}"; then
        probe "filesystem holding ${path}" df -B1 --output=source,fstype,size,used,avail,pcent,ipcent,target "${path}"
      fi
    done
    ;;
  *)
    printf 'probe-storage.bash: unknown mode %s (v1, v2 or df)\n' "${MODE}" >&2
    exit 2
    ;;
esac

if [[ "${FAILED}" -ne 0 ]]; then
  printf 'probe-storage.bash %s: at least one probe failed (see its rc in %s)\n' "${MODE}" "${REPORT}" >&2
  exit 1
fi
exit 0
