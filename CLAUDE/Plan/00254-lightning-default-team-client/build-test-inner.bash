#!/usr/bin/env bash
# build-test-inner.bash — Plan 00254 Task 2.2, the half that runs INSIDE the build container.
#
# build-test.bash mounts Lightning's source at /src (a clone at the pinned upstream commit),
# this plan folder at /plan (read-only) and the run directory at /out, and calls it twice:
#
#   red    apply only the test half of the patch, build, run the thread-panel QML test: it
#          must FAIL, proving the new case catches the clipped root on the old QML.
#   green  apply the QML half too, rebuild, run it again (it must pass), then build every
#          target and run every ctest whose name contains "thread" (none may fail).
#
# Each run's full output is kept as /out/<phase>-*.log; the last ctest lines go to stdout.
set -euo pipefail

readonly PATCH=/plan/lightning-thread-root.patch
readonly TEST_TARGET=thread-panel-pagination-anchor-qml-test
readonly TEST_NAME='^thread-panel-pagination-anchor-qml$'
phase="${1:?usage: build-test-inner.bash red|green}"

cd /src
git config --global --add safe.directory /src

build() {
  if [[ ! -d build ]]; then
    cmake -S . -B build -G Ninja -DCMAKE_BUILD_TYPE=Debug -DBUILD_TESTING=ON \
      -DLIGHTNING_ENABLE_WEBRTC=OFF -DLIGHTNING_DISABLE_CCACHE=ON >"/out/${phase}-configure.log" 2>&1
  fi
  cmake --build build --target "$@" -j "$(nproc)" >"/out/${phase}-build.log" 2>&1
}

runTests() {
  local regex="$1" log="$2"
  QT_QPA_PLATFORM=offscreen dbus-run-session -- ctest --test-dir build -R "${regex}" --output-on-failure >"${log}" 2>&1
}

report() { tee -a /out/build-test-report.txt; }

# expectPass REGEX LOG WHAT: run the tests; report their totals; fail the phase if any failed.
expectPass() {
  if runTests "$1" "$2"; then
    printf 'PASS: %s\n' "$3" | report
    grep -E 'Totals:|tests passed' "$2" | report
  else
    printf 'FAIL: %s (see %s)\n' "$3" "${2##*/}" | report
    grep -E 'Totals:|tests passed|FAIL!|Failed' "$2" | report
    exit 1
  fi
}

case "${phase}" in
  red)
    git apply --include='tests/*' "${PATCH}"
    build "${TEST_TARGET}"
    if runTests "${TEST_NAME}" /out/red-ctest.log; then
      printf 'RED NOT SHOWN: the new case passes on the unpatched QML (see red-ctest.log)\n' | report
      exit 1
    fi
    printf 'RED as expected: the new case fails on the unpatched QML\n' | report
    grep -E 'Totals:|tests passed|FAIL!' /out/red-ctest.log | report
    ;;
  green)
    git apply --include='qml/*' "${PATCH}"
    build "${TEST_TARGET}"
    expectPass "${TEST_NAME}" /out/green-ctest.log "the new case on the patched QML"
    build all
    ctest --test-dir build -N -R thread | grep -E 'Test +#|Total Tests' | report
    expectPass thread /out/green-thread-ctest.log "every ctest named *thread*, every target built"
    ;;
  *)
    printf 'build-test-inner.bash: unknown phase %s\n' "${phase}" >&2
    exit 2
    ;;
esac
