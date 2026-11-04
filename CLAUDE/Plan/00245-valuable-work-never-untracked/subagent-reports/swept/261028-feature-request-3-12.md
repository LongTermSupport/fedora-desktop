**Kind:** feature request (not a defect). Installed and latest: v3.71.0.

### The request

Let `secret_file_guard`'s Bash route allow a token that is a program's positional argument which cannot be a file read (not path-like, not an operand of a file-reading command), as the grep-pattern exemption already does for a search pattern.

### Why

The default `*.secret*` protected pattern matches any Bash token that merely contains `.secret`. The handler's guidance says this is deliberate and the accepted cost, so the deny below is intended behaviour, not a bug.

One case it catches reads no file at all. NetworkManager names one VPN connection setting with a single token: the word `vpn`, a dot, then the word `secrets`. It is passed as a positional setting name, for example `nmcli connection modify <conn>` followed by that setting name and a value. No file of that name is involved. The command is denied with R-SECRET-BASH-MENTION. The only way through is to build the token from parts at run time, which hides the text from the guard. That is worse for everyone than an exemption the guard can reason about.

### What the source shows

- `handlers/pre_tool_use/secret_file_guard.py:1421`: the Bash route runs `find_protected_mention_detail` over every word of the command. The only narrowings come after a match: a consumer in flag position, an encrypted target, and a grep-family search pattern.
- `utils/secret_file_matching.py:1461`: `find_protected_mention_detail` returns the first match.
- `utils/secret_file_matching.py:3269`: `is_grep_pattern_only_mention` already narrows by argument position, because "searching FOR a protected name's literal text is not reading the file".
- `secret_file_guard.py:1814-1819`: the "intentionally broad ... accepted cost" guidance.

### Configuration ruled out

- `mode: replace` with a narrower list: any glob that protects a file named `<x>.secrets` also matches the bare token, so the only fix is to drop protection for those files.
- `allowed_consumers`: it exempts a path in a consumer's flag position. This token is a positional setting name, not a flag value.

### Possible shape

Model it on `is_grep_pattern_only_mention`, kept to one fully literal simple command. A per-program list of positional slots that take a non-path value (here, `nmcli connection modify`'s setting names). Or a check that the matching token has no `/`, no leading `~` or `.`, and is not an operand of a known file reader (`cat`, `cp`, `source`, an interpreter, ...). Either keeps deny-by-default for anything that could open a file.
