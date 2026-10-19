# DBF search: key-only actions in Bubble Tea code

Instances: 0

## Searched
Scope: all *.go under the worktree, excluding src-go/vendor/, src-go/qa/rules/ and src-go/internal/fzfbind/ (the latter does not exist as a directory).
- rg/grep: key.NewBinding, key.WithHelp, key.WithKeys  -> one binding, src-go/internal/ui/frame/frame.go:26, with WithHelp("q","quit"); its footer is drawn at frame.go:63 via help.ShortHelpView. Not an instance.
- grep: tea.KeyMsg / KeyPressMsg / KeyReleaseMsg, Keystroke(), msg.Code, msg.Text, bubbletea -> only src-go/internal/ui/frame/frame.go:55 (inside internal/ui, exempt for item 2; matched via key.Matches, so item 3 is not hit) and frame_test.go (constructs messages, a test).
- grep: files importing charm.land / charmbracelet / tea.Msg -> only the two frame files. qa/godeps/run_test.go mentions bubbles only inside an error-string fixture.
- grep: `.String() ==/!=` and `case "ctrl|enter|esc|q|y|n"` literals -> hits are bytes.Buffer comparisons in tests, and src-go/internal/freeze/chooser.go:163 (`case "q","Q","quit"`), a line-read reply to a printed prompt "Choice [1-N, q to quit]", not a key message, and the hint is shown. Not an instance.
## Reading
Read frame.go in full: the single key action is printed in the footer.
Techniques: text search found everything; reading only confirmed the hint is rendered and that chooser.go is not a key message.
