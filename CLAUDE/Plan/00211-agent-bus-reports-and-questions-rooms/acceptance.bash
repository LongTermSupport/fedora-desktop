#!/usr/bin/env bash
# Plan 00211 — acceptance.bash: the verdict on each deploy (SPEC §11.2).
#
# STAGE 1 (deploy 1, the member side):
#   S1 the host's pingbus (/usr/local/bin/pingbus, installed by play-agent-bus.yml) and the one
#      staged into the ccy build context (/opt/claude-yolo/optional/agent-bus/pingbus, built by
#      play-claude-yolo.yml and baked into the image) both print this checkout's
#      `pingbus version` line: 0.2.0 is the release that knows the reports and questions rooms;
#   S2 every LXC container named by a present agent_bus_lxc_members entry is RUNNING and its
#      kit's pingbus prints that same line; a container that is down is refused, named.
# STAGE 2 (deploy 2, the admin side). The sessions it drives are the seats held in running ccy
# sessions (podman exec) and the declared LXC members (lxc-attach), _acceptance-stage2.inc.bash:
#   the joins: up to 180 s for every coordinator to have joined its questions room and every
#      seat held in a running ccy session its reports room (SPEC §5.3 leg 3, and A2's running
#      members), each that has not named;
#   A1, A2 `sudo agent-bus check-rooms <team>` passes for every present team: the rooms, the
#      index, the back-links, exact power levels and join rules, every human in, no worker in
#      questions, every coordinator joined to both rooms;
#   the coordinator: the first team whose registry names one, and the session it runs in, ccy
#      or LXC. No team with a coordinator, or a coordinator in no session this host can drive,
#      is REJECTED, named, before anything is changed;
#   A3 a throwaway worker in that team, added for the run as a host member on loopback and
#      removed again on cleanup, posts a `pingbus report`; with its token, reading, joining and
#      posting in the questions room are each refused 403 M_FORBIDDEN; after 15 s the report
#      is in the inbox of no session of the team;
#   A4 the first declared LXC member's `pingbus report`, run inside its container, is taken by
#      the homeserver. With no LXC member declared, A4 is reported NOT JUDGED, never PASS;
#   then the coordinator asks a test question in its own session, which the owner answers in
#   Element (ACCEPTANCE-owner.md).
# --after-owner (A5, after the owner's Element steps): the coordinator's
#   `pingbus history --room questions`, in its session, shows the owner's thread reply and its
#   edit attached to the test question, and its session took both (consumed, none dropped).
#   The raw lines are kept for Task 4.2.
#
# RUN ON THE HOST, as the desktop user (not root): deploy.bash --stage N runs --stage N as its
# last leg. The one prompt is sudo's, before the log opens (R3). `--fake` runs the verdict
# functions over fixtures it writes, anywhere (the CCY container included), changing nothing.
#
# WHAT IT CHANGES: stage 1 nothing. Stage 2 adds one worker to a team and removes it again
# (its account stays, deactivated, under a seat name used once), posts two reports and one
# test question. --after-owner nothing. It copies agent_bus_lxc_members and agent_bus_teams
# into its own run directory (0600) through ad-hoc Ansible.
#
# Never queue the plan folder itself in meta-deploy.list: that runs deploy.bash and this script
# with no --stage, which both refuse. Queue deploy-stage-2.bash or acceptance-after-owner.bash.
#
# EXIT CODES: 0 ACCEPTED; 1 REJECTED (a check failed and names itself); 64 usage.
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash --stage 1|2 [--fake] | --after-owner [-h|--help]

Plan 00211's verdict on each deploy:
  --stage 1      deploy 1: the host's, the ccy image's and every declared LXC member's pingbus
                 are this checkout's (S1, S2)
  --stage 2      deploy 2: the coordinators and running ccy seats have joined, every team's
                 side rooms are sound (A1, A2), a worker is kept out of questions and wakes
                 no inbox (A3), an LXC member reports (A4, or NOT JUDGED with none); then the
                 coordinator, in its ccy or LXC session, asks the test question
  --after-owner  after the Element steps of ACCEPTANCE-owner.md: the reply and its edit reached
                 the coordinator and its session took both (A5)
  --fake         the stage's verdicts over fixtures; runs anywhere, changes nothing
Exit 0 ACCEPTED, 1 REJECTED, 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"

STAGE=""
FAKE=0
AFTER_OWNER=0
args=("${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}")
i=0
while [[ "${i}" -lt "${#args[@]}" ]]; do
    case "${args[i]}" in
        --stage)
            i=$((i + 1))
            STAGE="${args[i]:-}"
            ;;
        --stage=*) STAGE="${args[i]#--stage=}" ;;
        --fake) FAKE=1 ;;
        --after-owner) AFTER_OWNER=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${args[i]}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
    i=$((i + 1))
done
if [[ "${AFTER_OWNER}" -eq 1 ]]; then
    if [[ -n "${STAGE}" ]] || [[ "${FAKE}" -eq 1 ]]; then
        printf '[FATAL] --after-owner takes no --stage and no --fake\n%s\n' "${PLAN_USAGE}" >&2
        exit 64
    fi
elif [[ "${STAGE}" != "1" ]] && [[ "${STAGE}" != "2" ]]; then
    printf '[FATAL] --stage 1, --stage 2 or --after-owner is required\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here (--fake runs the verdicts alone)\n' >&2
    exit 64
fi

# What the verdicts run. --fake points each at a fixture; nothing else differs.
HOST_PINGBUS="/usr/local/bin/pingbus"
STAGED_PINGBUS="/opt/claude-yolo/optional/agent-bus/pingbus"
LXC_INFO=(sudo -n lxc-info)
LXC_ATTACH=(sudo -n lxc-attach)
AGENT_BUS=(sudo -n agent-bus)
PODMAN=(podman)
CURL=(curl)
MEMBERS_JSON=""
TEAMS_JSON=""
REPORT=""
# A seat name used once: `a` and the month, day and time (the seat pattern allows 12 characters).
RUN_TAG="a$(date +%m%d%H%M%S)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-stage1.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-stage1.inc.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-stage2.inc.bash
source "${PLAN_SCRIPT_DIR}/_acceptance-stage2.inc.bash"

# check_leg <name> <cmd...> — one check, a bare top-level statement like plan_deploy_leg; the
# first that does not pass ends the run (exit 1).
check_leg() {
    local name="$1"
    shift
    printf '\n==> [check] %s\n' "${name}"
    if ! "$@"; then
        printf 'FAIL  %s\n' "${name}" >&2
        printf '## FAIL %s\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s\n' "${name}"
    printf '## PASS %s\n\n' "${name}" >>"${REPORT}"
}

# not_judged <name> <why> — a check that had nothing to run on: said, never reported as PASS.
not_judged() {
    printf '\n==> [check] %s\nNOT JUDGED  %s: %s\n' "$1" "$1" "$2"
    printf '## NOT JUDGED %s: %s\n\n' "$1" "$2" >>"${REPORT}"
}

# reject_leg <name> <cmd...> — the fake mode's negative check: the verdict must refuse it.
reject_leg() {
    local name="$1"
    shift
    printf '\n==> [must be rejected] %s\n' "${name}"
    if "$@"; then
        printf 'FAIL  %s: the verdict accepted it\n' "${name}" >&2
        printf '## FAIL %s (accepted)\n' "${name}" >>"${REPORT}"
        exit 1
    fi
    printf 'PASS  %s (rejected, as it must be)\n' "${name}"
    printf '## PASS %s (rejected)\n\n' "${name}" >>"${REPORT}"
}

readonly TWO='[{"team":"t1","container":"lxc-a","user":"u1"},{"team":"t2","container":"lxc-a","user":"u2"},{"team":"t1","container":"lxc-b","user":"u3","state":"present"}]'
readonly GONE='[{"team":"t1","container":"lxc-c","user":"u1","state":"absent"}]'
readonly TEAMS2='[{"team":"t1"},{"team":"t2","state":"present"},{"team":"t3","state":"absent"}]'
readonly SENT_LINE=$'SENT\t1\tt1\t$Abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG'
readonly HISTORY_TWO=$'HISTORY\t1\tin\t1\tHUMAN\tt1\t$e1\talice\t1\t"yes"\tquestions:Q3\nHISTORY\t1\tin\t2\tHUMAN\tt1\t$e2\talice\t2\t"yes, edited"\tquestions:Q3\nHISTORY\t1\tin\t3\tHUMAN\tt1\t$e3\talice\t3\t"other"\tquestions:Q2'
readonly HISTORY_ONE=$'HISTORY\t1\tin\t1\tHUMAN\tt1\t$e1\talice\t1\t"yes"\tquestions:Q3\nHISTORY\t1\tout\t2\tSENT\tt1\t$e2\nHISTORY\t1\tin\t3\tHUMAN\tt1\t$e3\talice\t3\t"edit of Q2"\tquestions:Q2'

if [[ "${FAKE}" -eq 1 ]]; then
    # STANDARD-EXCEPTION(R2): --fake reads only the fixtures it writes, so where it runs does not matter
    plan_start_log auto
    REPORT="${PLAN_RUN_DIR}/acceptance-fake-report.md"
    printf '# Plan 00211 acceptance, stage %s, fake mode\n\nRun directory: %s\n\n' "${STAGE}" "${PLAN_RUN_DIR}" >"${REPORT}"
    plan_deploy_leg "this checkout's pingbus version" want_version
    plan_deploy_leg "write the fixture commands" fake_setup
    if [[ "${STAGE}" == "1" ]]; then
        check_leg "S1 (fake) host and staged pingbus are new" fake_s1 new new
        reject_leg "S1 (fake) an old host pingbus" fake_s1 old new
        reject_leg "S1 (fake) an old staged pingbus" fake_s1 new old
        reject_leg "S1 (fake) a missing staged pingbus" fake_s1 new missing
        check_leg "S2 (fake) two running containers, both new, one shared by two members" \
            fake_s2 "${TWO}" lxc-a=RUNNING:new lxc-b=RUNNING:new
        check_leg "S2 (fake) no member declared" fake_s2 '[]'
        check_leg "S2 (fake) an absent member's container is not judged" fake_s2 "${GONE}" lxc-c=STOPPED:old
        reject_leg "S2 (fake) a stopped container" fake_s2 "${TWO}" lxc-a=RUNNING:new lxc-b=STOPPED:new
        reject_leg "S2 (fake) a running container with the old pingbus" \
            fake_s2 "${TWO}" lxc-a=RUNNING:old lxc-b=RUNNING:new
        printf '\nFAKE ACCEPTED: the stage-1 verdicts accept new versions and refuse old, missing and down ones\n' | tee -a "${REPORT}"
        plan_finish
    fi
    plan_deploy_leg "write the stage-2 fixture commands" fake2_setup
    check_leg "A1/A2 (fake) every present team's rooms are sound" fake_a12 "${TEAMS2}" t1=0 t2=0
    reject_leg "A1/A2 (fake) a team with a problem" fake_a12 "${TEAMS2}" t1=0 t2=1
    reject_leg "A1/A2 (fake) check-rooms cannot run" fake_a12 "${TEAMS2}" t1=70 t2=0
    reject_leg "A1/A2 (fake) no present team" fake_a12 '[{"team":"t3","state":"absent"}]'
    check_leg "the joins (fake) the coordinator and the running ccy seat joined" fake_join join join
    check_leg "the joins (fake) a team with no coordinator" fake_join_nocoord
    reject_leg "the joins (fake) the coordinator is only invited" fake_join invite join
    reject_leg "the joins (fake) the coordinator rejected the invite" fake_join leave join
    reject_leg "the joins (fake) the coordinator has no ROOM line" fake_join absent join
    reject_leg "the joins (fake) a running ccy seat has not joined reports" fake_join join invite
    check_leg "the coordinator (fake) found in its ccy session" fake_pick alpha ccy
    check_leg "the coordinator (fake) found as an LXC member" fake_pick alpha lxc
    reject_leg "the coordinator (fake) in no session this host can drive" fake_pick alpha none
    reject_leg "the coordinator (fake) no team has one" fake_pick none ccy
    check_leg "A3 (fake) the report is in no inbox" inbox_clear "a fixture inbox" $'STATE\tpresent\n$r1\t-\tkept'
    reject_leg "A3 (fake) the report reached an inbox" inbox_clear "a fixture inbox" $'STATE\tpresent\n$r1\tinbox\tkept'
    reject_leg "A3 (fake) the report was consumed" inbox_clear "a fixture inbox" $'STATE\tpresent\n$r1\tconsumed\tkept'
    reject_leg "A3 (fake) a session with no state directory proves nothing" \
        inbox_clear "a fixture inbox" $'STATE\tmissing\n$r1\t-\tkept'
    check_leg "A3 (fake) 403 M_FORBIDDEN is a refusal" fake_refused 403 '{"errcode":"M_FORBIDDEN","error":"no"}'
    reject_leg "A3 (fake) 200 is no refusal" fake_refused 200 '{"event_id":"x"}'
    reject_leg "A3 (fake) another 403 is no refusal" fake_refused 403 '{"errcode":"M_UNKNOWN_TOKEN"}'
    reject_leg "A3 (fake) a non-JSON answer" fake_refused 403 'not json'
    check_leg "A3/A4 (fake) a SENT line" sent_ok "a fixture report" "${SENT_LINE}"
    reject_leg "A3/A4 (fake) no SENT line" sent_ok "a fixture report" $'DROPPED\t1\t1'
    check_leg "A5 (fake) the reply and its edit attached to Q3" replies_ok Q3 "${HISTORY_TWO}"
    reject_leg "A5 (fake) one reply only (the other lines are another question and a send)" \
        replies_ok Q3 "${HISTORY_ONE}"
    check_leg "A5 (fake) the attached events are Q3's two" attached_are "\$e1 \$e2" Q3 "${HISTORY_TWO}"
    check_leg "A5 (fake) the session took both" envelopes_ok "a fixture session" \
        $'STATE\tpresent\n$e1\tconsumed\tkept\n$e2\tconsumed\tkept'
    reject_leg "A5 (fake) one is still in the inbox" envelopes_ok "a fixture session" \
        $'STATE\tpresent\n$e1\tconsumed\tkept\n$e2\tinbox\tkept'
    reject_leg "A5 (fake) one was dropped" envelopes_ok "a fixture session" \
        $'STATE\tpresent\n$e1\tconsumed\tkept\n$e2\tconsumed\tdropped'
    reject_leg "A5 (fake) one never arrived" envelopes_ok "a fixture session" \
        $'STATE\tpresent\n$e1\tconsumed\tkept\n$e2\t-\tkept'
    not_judged "A4 (fake) the NOT JUDGED line" "printed for a stage with no LXC member, never PASS"
    printf '\nFAKE ACCEPTED: the stage-2 and A5 verdicts accept sound fixtures and refuse each fault\n' | tee -a "${REPORT}"
    plan_finish
fi

plan_require_host "it reads the host's pingbus, the team homeserver and the LXC containers"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for agent-bus and the LXC tools\n' >&2
    exit 1
fi
for tool in jq python3 lxc-info lxc-attach podman ansible curl agent-bus; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed: its play has not run on this host\n' "${tool}" >&2
        exit 1
    fi
done

plan_prime_sudo
plan_start_log auto
REPORT="${PLAN_RUN_DIR}/acceptance-report.md"
MEMBERS_JSON="${PLAN_RUN_DIR}/lxc-members.json"
TEAMS_JSON="${PLAN_RUN_DIR}/teams.json"

if [[ "${AFTER_OWNER}" -eq 1 ]]; then
    printf '# Plan 00211 acceptance, after the owner (A5)\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
    plan_deploy_leg "read agent_bus_lxc_members" read_members
    plan_deploy_leg "the sessions this host can drive (ccy seats, LXC members)" read_sessions
    check_leg "A5 the owner's reply and its edit reached the coordinator, and its session took both" a5_check
    printf '\nACCEPTED: the questions room carries a human answer and its edit to the coordinator\n' | tee -a "${REPORT}"
    plan_finish
fi

printf '# Plan 00211 acceptance, stage %s\n\nRun directory: %s\n\n' "${STAGE}" "${PLAN_RUN_DIR}" >"${REPORT}"
plan_deploy_leg "this checkout's pingbus version" want_version
plan_deploy_leg "read agent_bus_lxc_members" read_members
if [[ "${STAGE}" == "1" ]]; then
    check_leg "S1 the host's pingbus and the ccy image's staged pingbus are this checkout's" s1_check
    check_leg "S2 every declared LXC member's pingbus is this checkout's, and none is down" s2_check
    printf '\nACCEPTED: deploy 1 is on the host, in the ccy build context and in every declared LXC container\n' | tee -a "${REPORT}"
    plan_finish
fi

plan_deploy_leg "read agent_bus_teams" read_teams
plan_deploy_leg "the sessions this host can drive (ccy seats, LXC members)" read_sessions
# The wait runs here, straight after this script primed sudo, not after the image rebuild.
check_leg "the joins: every coordinator joins questions, every running ccy seat reports (up to ${JOIN_WAIT_S} s)" rooms_joined
check_leg "A1/A2 every team's side rooms are sound (agent-bus check-rooms)" a12_check
check_leg "the coordinator: a team has one, in a session this host can drive" pick_coordinator
plan_deploy_leg "A3 add a throwaway worker (removed on cleanup)" a3_take
check_leg "A3 a worker reports, and is refused the questions room" a3_check
check_leg "A3 the worker's report is in no inbox of its team" a3_inbox_check
a4_said="took an LXC member's report"
if [[ -n "$(a4_member)" ]]; then
    check_leg "A4 an LXC member reports from inside its container" a4_check
else
    not_judged "A4 an LXC member reports from inside its container" \
        "no agent_bus_lxc_members entry is present, so this host has no LXC member to report"
    a4_said="did not judge A4 (no LXC member is declared)"
fi
check_leg "the test question, asked by the coordinator in its own session" ask_test_question
printf '\nACCEPTED: deploy 2 made every team'\''s side rooms, kept a worker out of questions and every inbox, %s, and asked the test question\n' \
    "${a4_said}" | tee -a "${REPORT}"
printf '\n==> NEXT (the owner): the Element steps in ACCEPTANCE-owner.md, then meta-deploy runs\n'
printf '    acceptance-after-owner.bash (A5).\n'
plan_finish
