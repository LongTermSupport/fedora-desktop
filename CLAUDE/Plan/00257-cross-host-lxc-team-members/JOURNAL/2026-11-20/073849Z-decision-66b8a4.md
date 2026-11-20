## 07:38 · decision · —

Decision: the #294 guard is FROZEN at its simplest safe form. There is no round 5 of the inert/segment approach.

What the frozen form is. `ansible_enforcement` judges the RAW Bash text, as the original handler did. The original eighteen patterns are kept verbatim, and the additions only add denials: options before the verb, `dnf5`, absolute or quoted tool names, pip variants, `systemctl --system`, and quote-split words such as `d''nf`. The inert exception, every helper and allow-list that served it, and the segment walk for it are deleted. The one raw judge stays in `_raw_command_judge` (about forty lines): the raw text first, then each command-position segment, with and without its words resolved. `hidden_git_mutation` keeps its round 1 fix and `system_paths` its rule id and fixes; the reviews found both correct.

Why. Four rounds of an exception for text-only mentions each opened real runs that the original denied. The round 4 review found three more: heredoc delimiter forms, `>&2f` writing a file, and a bare `--pretty` hiding `--output=`. Each round closed the classes the previous review named and a new review found another, so the exception cannot be made safe by listing shapes. Right-sizing ruling: a guard that never lets through what the original denied, and blocks some harmless text, is the better trade for this repository than a guard that is clever and wrong.

How it is held. A real oracle invariant test: the original pattern list is copied verbatim into the test as a frozen constant, and for every line of a corpus (every probe template of review rounds 3 and 4, each filled with every change, plus the hand lists) a line the original matched must be denied. The oracle never calls the code under test. The original patterns are also kept as the first entries of the handler's list, so the property holds by construction, not only by test.

Known gap. A command that only MENTIONS a blocked tool in text (a commit message, an echo, a grep pattern) is blocked too. The deny message says so and gives the workaround: write the text to a file with the Write tool and pass it by path (`git commit -F`, `--body-file`). The cases are pinned as known limits in one named test list.

Issue #294 stays open for a parser-grade approach later: a real shell parse that knows which words a command runs, with the oracle test above as its acceptance bar.

Semgrep. The one sanctioned judge stays the visible exception. The path exclusion on the taint rule was dead configuration (the judge's parameter is not a taint source), so it is gone; what protects the judge is its name at the call site plus the impostor rule's check on where that name is defined or imported. The taint rule now also catches a look-alike called through an imported module (`module.func(command)`).
