package readywait

// Cases for the shell rule, one group per expectation of SPEC-qa-ready-wait-rules.md.

type ruleCase struct {
	name   string
	spec   string
	script string
	want   []int
}

var ruleCases = []ruleCase{
	{name: "a_poll_after_a_background_start_is_reported", spec: "R15, R16, R18", script: `tmate -F &
TMATE_PID=$!
for _ in $(seq 1 30); do
    tmate show-messages | grep -q "read only" && break
    sleep 0.5
done
`, want: []int{3}},
	{name: "a_while_poll_on_a_port_file_is_reported", spec: "R15, R16, R18", script: `python3 proxy.py "$port_file" &
proxy_pid=$!
while [ ! -s "$port_file" ]; do
    sleep 0.1
done
`, want: []int{3}},
	{name: "a_wait_helper_called_after_a_launcher_is_reported", spec: "R15, R16, R18", script: `wait_for_state() {
    local _
    for _ in $(seq 1 100); do
        grep -q RECORDING "$log" && return 0
        sleep 0.05
    done
    return 1
}
start_it() {
    program > "$out" 2> "$err" &
    PROGRAM_PID=$!
}
start_it
if wait_for_state; then
    kill -TERM "$PROGRAM_PID"
fi
`, want: []int{3}},
	{name: "kill_0_in_the_loop_clears_it", spec: "R19, R20", script: `tmate -F &
TMATE_PID=$!
for _ in $(seq 1 30); do
    if ! kill -0 "$TMATE_PID" 2>/dev/null; then
        wait "$TMATE_PID"
        exit 1
    fi
    sleep 0.5
done
`, want: []int{}},
	{name: "kill_0_in_the_loop_condition_clears_it", spec: "R19, R20", script: `server &
pid=$!
while kill -0 "$pid" && [ ! -S "$sock" ]; do
    sleep 0.1
done
`, want: []int{}},
	{name: "kill_0_only_after_the_loop_does_not_clear_it", spec: "R19, R20", script: `tmate -F &
TMATE_PID=$!
for _ in $(seq 1 30); do
    tmate show-messages | grep -q "read only" && break
    sleep 0.5
done
if kill -0 "$TMATE_PID"; then kill "$TMATE_PID"; fi
`, want: []int{3}},
	{name: "wait_n_and_ps_p_count_as_liveness", spec: "R19, R20", script: `a &
pid=$!
for _ in 1 2 3; do
    ps -p "$pid" >/dev/null || exit 1
    sleep 1
done
for _ in 1 2 3; do
    wait -n -p done_pid "$pid" || exit 1
    sleep 1
done
`, want: []int{}},
	{name: "a_poll_with_no_background_start_is_not_reported", spec: "R18, R21, R22", script: `for _ in $(seq 1 30); do
    curl -fs http://localhost && break
    sleep 1
done
`, want: []int{}},
	{name: "a_poll_before_the_start_is_not_reported", spec: "R18, R21, R22", script: `until [ -e "$flag" ]; do
    sleep 1
done
worker &
pid=$!
`, want: []int{}},
	{name: "a_loop_without_sleep_is_not_reported", spec: "R18, R21, R22", script: `worker &
pid=$!
for f in *.txt; do
    echo "$f"
done
`, want: []int{}},
	{name: "a_poll_after_the_child_was_waited_for_is_not_reported", spec: "R18, R21, R22", script: `pw-record "$file" &
REC_PID=$!
wait "$REC_PID"
for _ in $(seq 1 20); do
    gdbus call --method Ask && break
    sleep 0.1
done
`, want: []int{}},
	{name: "a_fire_and_forget_helper_does_not_arm_its_callers", spec: "R18, R21, R22", script: `emit_state() {
    gdbus emit --signal StateChanged "$1" &
}
emit_state READY
for _ in $(seq 1 20); do
    gdbus call --method Ask && break
    sleep 0.1
done
`, want: []int{}},
	{name: "case_arms_are_alternatives", spec: "R18, R21, R22", script: `case "$1" in
    capture)
        reader &
        pid=$!
        ;;
    watch)
        while true; do
            cat /sys/power/state
            sleep 1
        done
        ;;
esac
`, want: []int{}},
	{name: "a_sampler_that_runs_its_full_course_is_not_a_wait", spec: "R18, R21, R22", script: `recorder &
pid=$!
for ((i = 0; i < secs; i++)); do
    sleep 1
    snap
done
end=$((SECONDS + secs))
while ((SECONDS < end)); do
    cat state
    sleep 0.1
done
while true; do
    date
    sleep 1
done
`, want: []int{}},
	{name: "a_while_true_that_breaks_is_a_wait", spec: "R18, R21, R22", script: `server &
pid=$!
while true; do
    [ -S "$sock" ] && break
    sleep 0.1
done
`, want: []int{3}},
	{name: "a_function_run_in_the_background_is_a_start_not_a_wait", spec: "R18, R21, R22", script: `watch_it() {
    while [ -e "$flag" ]; do
        sleep 1
    done
}
reader &
pid=$!
watch_it "$x" >"$out" 2>&1 &
`, want: []int{}},
	{name: "a_backgrounded_loop_is_not_a_wait_for_itself", spec: "R18, R21, R22", script: `while true; do
    date
    sleep 1
done &
`, want: []int{}},
	{name: "redirections_and_and_lists_are_not_launches", spec: "R14, R15", script: `exec {fd}<&0 </dev/null
echo hi >&2 && echo there &>/dev/null
cmd 2>&1 |& cat
for _ in 1 2 3; do
    [ -e "$ready" ] && break
    sleep 1
done
`, want: []int{}},
	{name: "an_ampersand_in_a_string_or_comment_is_not_a_launch", spec: "R14, R15", script: `echo "run it &"   # then wait &
printf '%s &\n' x
for _ in 1 2 3; do
    [ -e "$ready" ] && break
    sleep 1
done
`, want: []int{}},
	{name: "an_ampersand_in_a_heredoc_is_not_a_launch", spec: "R14, R15", script: `cat > "$f" <<'EOF'
server &
EOF
for _ in 1 2 3; do
    [ -e "$ready" ] && break
    sleep 1
done
`, want: []int{}},
	{name: "a_poll_after_lxc_start_is_reported", spec: "R17, R19", script: `if ! sudo lxc-start -n "$name"; then
    exit 1
fi
for _ in $(seq 1 30); do
    ip=$(sudo lxc-info -n "$name" -iH) || ip=""
    [ -n "$ip" ] && break
    sleep 1
done
`, want: []int{4}},
	{name: "a_poll_after_virt_install_is_reported", spec: "R17, R19", script: `virt-install --connect "$uri" --name "$dom" \
    --import --noautoconsole
while ! ssh -p "$port" guest true; do
    sleep 5
done
`, want: []int{3}},
	{name: "a_poll_after_virsh_start_with_options_is_reported", spec: "R17, R19", script: `virsh -c "$uri" start "$dom" >&2
until ssh guest true; do
    sleep 5
done
`, want: []int{2}},
	{name: "a_start_in_a_command_substitution_is_a_start", spec: "R17, R19", script: `out="$(sudo -n lxc-start -n "$ct" -d 2>&1)" || exit 1
until [ -e "$ready" ]; do
    sleep 1
done
`, want: []int{2}},
	{name: "a_function_that_starts_a_guest_arms_its_callers", spec: "R17, R19", script: `boot_guest() {
    virt-install --name "$dom" --import --noautoconsole
}
wait_for_ssh() {
    while ! ssh guest true; do
        sleep 5
    done
}
boot_guest
wait_for_ssh
`, want: []int{5}},
	{name: "the_tool_named_as_an_argument_is_not_a_start", spec: "R17, R19", script: `have_tool virt-install
command -v lxc-start >/dev/null
for probe_bin in lxc-ls lxc-start lxc-stop; do
    echo "$probe_bin"
done
virsh -c "$uri" autostart "$dom"
until [ -e "$ready" ]; do
    sleep 1
done
`, want: []int{}},
	{name: "lxc_info_state_in_the_loop_clears_it", spec: "R17, R19", script: `sudo lxc-start -n "$name"
while [ "$elapsed" -lt 30 ]; do
    state=$(sudo lxc-info -n "$name" -sH)
    [ "$state" = RUNNING ] || exit 1
    ip=$(sudo lxc-info -n "$name" -iH) || ip=""
    [ -n "$ip" ] && return 0
    sleep 1
done
`, want: []int{}},
	{name: "lxc_ls_running_clears_it", spec: "R17, R19", script: `sudo lxc-start -n "$name"
until [ -e "$ready" ]; do
    sudo lxc-ls --running | grep -qx "$name" || exit 1
    sleep 1
done
`, want: []int{}},
	{name: "virsh_domstate_and_list_clear_it", spec: "R17, R19", script: `virsh -c "$uri" start "$dom"
while state="$(virsh -c "$uri" domstate "$dom")" && ! ssh guest true; do
    sleep 5
done
until ssh guest true; do
    virsh list --name | grep -qx "$dom" || exit 1
    sleep 5
done
`, want: []int{}},
	{name: "a_helper_in_the_loop_header_that_asks_virsh_clears_it", spec: "R19", script: `guest_must_be_up() {
    state="$(virsh -c "$uri" domstate "$dom")" || die "no state"
    case "$state" in "shut off" | crashed) die "guest is $state" ;; esac
}
virt-install --name "$dom" --import --noautoconsole
while guest_must_be_up SSH && ! ssh guest true; do
    sleep 5
done
`, want: []int{}},
	{name: "a_helper_in_the_loop_body_that_kills_0_clears_it", spec: "R19", script: `still_alive() {
    kill -0 "$SERVER_PID" || exit 1
}
server &
SERVER_PID=$!
until [ -S "$sock" ]; do
    still_alive
    sleep 0.1
done
`, want: []int{}},
	{name: "a_helper_with_no_liveness_does_not_clear_it", spec: "R19", script: `guest_ssh() {
    ssh -p "$port" guest "$@"
}
virsh start "$dom"
until guest_ssh true; do
    sleep 5
done
`, want: []int{5}},
	{name: "only_one_level_of_helper_is_followed", spec: "R19", script: `ask_state() {
    virsh domstate "$dom"
}
guest_up() {
    ask_state
}
virsh start "$dom"
until ssh guest true; do
    guest_up
    sleep 5
done
`, want: []int{8}},
	{name: "nested_quotes_in_a_command_substitution_stay_code", spec: "R14", script: `check "x" "$([ -e "$events/a" ] && echo present || echo absent)"
worker &
pid=$!
for _ in 1 2 3; do
    [ -e "$ready" ] && break
    sleep 1
done
`, want: []int{4}},
	{name: "a_dollar_hash_is_not_a_comment", spec: "R14", script: `[ $# -gt 0 ] && n=${#items[@]}; worker &
pid=$!
for _ in 1 2 3; do
    [ -e "$ready" ] && break
    sleep 1
done
`, want: []int{3}},
}
