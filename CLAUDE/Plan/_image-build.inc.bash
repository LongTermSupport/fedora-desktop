# shellcheck shell=bash
#
# _image-build.inc.bash — sourced by plan scripts that launch ccy and wait for its container.
#
# A ccy launch may first build an image: the project image after the base image changed (a
# 24-step build), or an in-place Claude Code update. It holds that image's build lock while it
# builds, and the build takes as long as it takes, so a start wait counts its time from the end
# of any build, never across one. Sourced, never executed: no shell options, no `exit`. Tests:
# scripts/test-planlib.bash, which drives the launcher's own lib/image-build-lock.bash.

# plan_image_build — a ccy image build in progress on this host: prints `<pid> <image>` (`?`
# for the pid of a build that has not written its record yet) and returns 0; 3 with none; 1
# when a lock cannot be read. Judged as `ccy --build-lock` judges the locks
# lib/image-build-lock.bash takes ($XDG_RUNTIME_DIR/ccy/image-builds/*.lock): a build runs while
# its lock is held, unless the record names a process that has gone, or one started after the
# build began (a reused pid). Like `ccy --build-lock`, the probe takes a shared lock for an
# instant when nothing holds it.
plan_image_build() {
    local dir file rc line pid began image state age
    dir="${XDG_RUNTIME_DIR:-/run/user/${EUID}}/ccy/image-builds"
    if [[ ! -e "${dir}" ]]; then
        return 3
    fi
    if [[ ! -d "${dir}" || ! -r "${dir}" || ! -x "${dir}" ]]; then
        printf 'the image build lock directory %s cannot be read\n' "${dir}" >&2
        return 1
    fi
    for file in "${dir}"/*.lock; do
        [[ -e "${file}" ]] || continue
        rc=0
        flock -n -s -E 75 "${file}" true || rc=$?
        case "${rc}" in
            0) continue ;;
            75) ;;
            *)
                printf 'the image build lock %s cannot be probed (flock exit %d)\n' "${file}" "${rc}" >&2
                return 1
                ;;
        esac
        line="$(<"${file}")" || return 1
        read -r pid began image _ <<<"${line}"
        if [[ ! "${pid} ${began}" =~ ^[0-9]+\ [0-9]+$ ]]; then
            image="${file##*/}"
            image="${image%.lock}"
            printf '? %s\n' "${image/_/:}"
            return 0
        fi
        # ps exits non-zero when the process has gone: a stale record.
        state="$(ps -o stat= -o etimes= -p "${pid}")" || continue
        read -r state age <<<"${state}"
        if [[ "${state}" != [ZX]* ]] && ((EPOCHSECONDS - age <= began + 1)); then
            printf '%s %s\n' "${pid}" "${image}"
            return 0
        fi
    done
    return 3
}

# When plan_image_build_grace next says a build is running, in SECONDS.
PLAN_IMAGE_BUILD_SAY_AT=0

# plan_image_build_grace <what> <timeout-s> — for a start wait of <timeout-s>: 0 while an image
# build runs, so the caller starts its wait again, saying so on stderr at most once a minute
# with how to see and stop a build that has hung; 3 with none; 1 when the locks cannot be read.
# Any ccy image build counts, not only the launch's own: a launch also waits for another's
# build of its image, and a wait that runs longer can delay a failure but never pass a check.
plan_image_build_grace() {
    local build rc=0
    build="$(plan_image_build)" || rc=$?
    case "${rc}" in
        0)
            if ((SECONDS >= PLAN_IMAGE_BUILD_SAY_AT)); then
                printf '==> %s: waiting for the image build of %s (pid %s) to end; the %d s wait starts again then (if it has hung: see it and stop it with ccy --build-lock)\n' \
                    "$1" "${build#* }" "${build%% *}" "$2" >&2
                PLAN_IMAGE_BUILD_SAY_AT=$((SECONDS + 60))
            fi
            return 0
            ;;
        3) return 3 ;;
        *)
            printf '[FAIL] %s: the image build locks could not be read (above)\n' "$1" >&2
            return 1
            ;;
    esac
}
