package readywait

const sleepingWait = "until [ -e r ]; do\n    sleep 1\ndone\n"

// reviewCases are the cases the review of the first port added; each names the expectation it
// pins.
var reviewCases = []ruleCase{
	// R16: a function that keeps $! in any form is a launcher.
	{name: "a_pid_kept_in_a_quoted_assignment_is_a_launcher", spec: "R16", want: []int{6}, script: "start() {\n    w &\n    local pid=\"$!\"\n}\nstart\n" + sleepingWait},
	{name: "a_pid_appended_to_an_array_is_a_launcher", spec: "R16", want: []int{6}, script: "start() {\n    w &\n    pids+=(\"$!\")\n}\nstart\n" + sleepingWait},
	{name: "a_pid_written_to_a_file_is_a_launcher", spec: "R16", want: []int{6}, script: "start() {\n    w &\n    printf '%s' \"$!\" > pidfile\n}\nstart\n" + sleepingWait},
	{name: "a_dollar_bang_in_single_quotes_is_not_a_kept_pid", spec: "R16", want: []int{}, script: "start() {\n    w &\n    echo '$!'\n}\nstart\n" + sleepingWait},

	// R14: here-document delimiters are bash words, not identifiers.
	{name: "a_delimiter_with_a_dash_ends_at_its_own_line", spec: "R14", want: []int{6}, script: "cat <<END-MARK\nbody\nEND-MARK\nw &\np=$!\n" + sleepingWait},
	{name: "a_backslash_delimiter_makes_a_here_document", spec: "R14", want: []int{}, script: "cat <<\\EOF\nserver &\nEOF\n" + sleepingWait},
	{name: "a_quoted_delimiter_with_a_space", spec: "R14", want: []int{6}, script: "cat <<\"a b\"\nbody &\na b\nw &\np=$!\n" + sleepingWait},
	{name: "a_heredoc_terminator_named_done_is_not_a_loop_end", spec: "R14", want: []int{}, script: "w &\np=$!\ncat <<done\nsleep 1\ndone\n" + "echo ok\n"},

	// R14: a hash right after a closing quote is part of the word.
	{name: "a_hash_after_a_closing_quote_is_not_a_comment", spec: "R14", want: []int{3}, script: "echo \"a\"#b; w &\np=$!\n" + sleepingWait},
	{name: "a_hash_after_a_single_quote_is_not_a_comment", spec: "R14", want: []int{3}, script: "echo 'a'#b; w &\np=$!\n" + sleepingWait},

	// R15: a lone & in arithmetic is a bit-and.
	{name: "a_bit_and_in_arithmetic_expansion_is_not_a_start", spec: "R15", want: []int{}, script: "x=$(( a & 1 ))\n" + sleepingWait},
	{name: "a_bit_and_in_an_arithmetic_command_is_not_a_start", spec: "R15", want: []int{}, script: "((!(8#${mode} & 8#022)))\n" + sleepingWait},
	{name: "a_bit_and_in_a_c_style_loop_is_not_a_start", spec: "R15", want: []int{}, script: "for ((i = 0; i < (n & 3); i++)); do\n    echo \"$i\"\ndone\n" + sleepingWait},
	{name: "an_escaped_ampersand_is_not_a_start", spec: "R15", want: []int{}, script: "echo a\\&b\n" + sleepingWait},

	// R22: a loop that is itself backgrounded is a start, armed or not.
	{name: "a_backgrounded_loop_in_an_armed_scope_is_not_a_wait", spec: "R22", want: []int{}, script: "w &\np=$!\nwhile true; do\n    [ -e r ] && break\n    sleep 1\ndone &\n"},

	// R17: what may come before a manager start.
	{name: "sudo_with_a_valued_option_before_a_start", spec: "R17", want: []int{2}, script: "sudo -u root lxc-start -n c\n" + sleepingWait},
	{name: "nohup_before_a_start", spec: "R17", want: []int{2}, script: "nohup lxc-start -n c\n" + sleepingWait},
	{name: "exec_before_a_start", spec: "R17", want: []int{2}, script: "exec virt-install --name d\n" + sleepingWait},
	{name: "an_assignment_prefix_before_a_start", spec: "R17", want: []int{2}, script: "LIBVIRT_DEFAULT_URI=qemu:///session virsh start d\n" + sleepingWait},
	{name: "a_negated_start", spec: "R17", want: []int{2}, script: "! lxc-start -n c || exit 1\n" + sleepingWait},

	// R19: the forms of a liveness check.
	{name: "kill_s_0_clears_it", spec: "R19", want: []int{}, script: "w &\np=$!\nuntil [ -e r ]; do\n    kill -s 0 \"$p\" || exit 1\n    sleep 1\ndone\n"},
	{name: "kill_signal_0_clears_it", spec: "R19", want: []int{}, script: "w &\np=$!\nuntil [ -e r ]; do\n    kill --signal 0 \"$p\" || exit 1\n    sleep 1\ndone\n"},
	{name: "lxc_info_state_long_option_clears_it", spec: "R19", want: []int{}, script: "lxc-start -n c\nuntil [ -e r ]; do\n    lxc-info -n c --state | grep -q RUNNING || exit 1\n    sleep 1\ndone\n"},
	{name: "lxc_ls_active_clears_it", spec: "R19", want: []int{}, script: "lxc-start -n c\nuntil [ -e r ]; do\n    lxc-ls --active | grep -qx c || exit 1\n    sleep 1\ndone\n"},
	{name: "kill_without_a_zero_signal_does_not_clear_it", spec: "R19", want: []int{3}, script: "w &\np=$!\nuntil [ -e r ]; do\n    kill -9 \"$p\"\n    sleep 1\ndone\n"},

	// R21: a function with the function keyword and no parentheses is a function.
	{name: "function_keyword_launcher_arms_its_callers", spec: "R21", want: []int{6}, script: "function start_it {\n    w &\n    p=$!\n}\nstart_it\n" + sleepingWait},
	{name: "function_keyword_launcher_does_not_arm_the_top_level", spec: "R21", want: []int{}, script: "function start_it {\n    w &\n    p=$!\n}\n" + sleepingWait},
}
