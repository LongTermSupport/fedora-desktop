package freeze

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// MemberScreen is the drill-down for one group. Lines[0] is the "all" row; Lines[i+1] belongs to
// Names[i].
type MemberScreen struct {
	Lines []string
	Names []string
}

// Choice is the drill-down's answer: the whole group, or just Names.
type Choice struct {
	All   bool
	Names []string
}

// Chooser asks the person. Group returns the chosen key or ErrQuit; Members returns the choice
// or ErrBack. Any other error is a failure (a missing terminal, a crashed fzf, an interrupt).
type Chooser interface {
	Group(ctx context.Context, keys, labels []string) (string, error)
	Members(ctx context.Context, screen MemberScreen) (Choice, error)
}

// Fzf is the picker the choosers use; *ui.Picker is the production one.
type Fzf interface {
	Choose(ctx context.Context, spec ui.ChooseSpec) ([]string, error)
}

// TTYChooser is the production Chooser: fzf when installed, else numbered prompts read from the
// controlling terminal (never stdin, so a piped stdin cannot answer for the person). Close it
// when the run ends.
type TTYChooser struct {
	Cfg Config
	Env Env
	Fzf Fzf

	tty    *bufio.Reader
	closer io.Closer
	say    *term.Term
}

// Close releases the terminal if one was opened.
func (c *TTYChooser) Close() error {
	if c.closer == nil {
		return nil
	}
	err := c.closer.Close()
	c.tty, c.closer = nil, nil
	if err != nil {
		return fmt.Errorf("close terminal: %w", err)
	}
	return nil
}

// Group implements Chooser.
func (c *TTYChooser) Group(ctx context.Context, keys, labels []string) (string, error) {
	if err := c.guard(); err != nil {
		return "", err
	}
	key, err := c.group(ctx, keys, labels)
	return key, c.withWrites(err)
}

// Members implements Chooser.
func (c *TTYChooser) Members(ctx context.Context, screen MemberScreen) (Choice, error) {
	if err := c.guard(); err != nil {
		return Choice{}, err
	}
	choice, err := c.members(ctx, screen)
	return choice, c.withWrites(err)
}

func (c *TTYChooser) group(ctx context.Context, keys, labels []string) (string, error) {
	if c.Fzf != nil {
		lines, fzfErr := c.Fzf.Choose(ctx, ui.ChooseSpec{
			Prompt: c.Cfg.Tool + ": ", Header: "ENTER acts on the chosen group — ESC to quit",
			Height: "50%", Rows: labels,
		})
		switch {
		case fzfErr == nil:
			return matchLabel(keys, labels, lines)
		case errors.Is(fzfErr, ui.ErrLeave):
			return "", ErrQuit
		case !errors.Is(fzfErr, ui.ErrFzfMissing):
			return "", fzfErr
		}
	}
	return c.numberedGroup(ctx, keys, labels)
}

// guard requires a real terminal on stderr and opens the controlling one. It is a named failure
// rather than a read of whatever is on stdin: a menu answered by a pipe would act on a group
// nobody chose.
func (c *TTYChooser) guard() error {
	if c.say == nil {
		c.say = &term.Term{Err: c.Env.Err}
	}
	if !c.Env.IsTerminal(c.Env.Err) {
		return c.noTerminal()
	}
	if c.tty != nil {
		return nil
	}
	rc, err := c.Env.OpenTTY()
	if err != nil {
		return c.noTerminal()
	}
	c.tty, c.closer = bufio.NewReader(rc), rc
	return nil
}

func (c *TTYChooser) noTerminal() error {
	return Die("no terminal to ask on — name a target explicitly: %s.\n  See '%s --help'.", c.Cfg.TargetHint, c.Cfg.Tool)
}

// withWrites surfaces a failed write to the prompt stream.
func (c *TTYChooser) withWrites(err error) error {
	if c.say.WriteErr != nil && err == nil {
		err = fmt.Errorf("write prompt: %w", c.say.WriteErr)
	}
	c.say.WriteErr = nil
	return err
}

// matchLabel maps fzf's chosen line back to its key by whole-string equality.
func matchLabel(keys, labels, lines []string) (string, error) {
	if len(lines) > 0 {
		for i, l := range labels {
			if l == lines[0] {
				return keys[i], nil
			}
		}
	}
	return "", Die("internal error: chosen entry not found.")
}

func (c *TTYChooser) numberedGroup(ctx context.Context, keys, labels []string) (string, error) {
	c.say.Printf("What to act on:\n")
	for i, l := range labels {
		c.say.Printf("  %2d) %s\n", i+1, l)
	}
	for attempt := 1; ; attempt++ {
		c.say.Printf("Choice [1-%d, q to quit]: ", len(labels))
		reply, err := c.readLine(ctx)
		if errors.Is(err, io.EOF) {
			c.say.Printf("\n")
			return "", ErrQuit
		}
		if err != nil {
			return "", err
		}
		switch reply {
		case "q", "Q", "quit":
			return "", ErrQuit
		}
		if n, ok := parseNumber(reply); ok && n >= 1 && n <= len(labels) {
			return keys[n-1], nil
		}
		c.say.Printf("  Not one of the options — enter a number between 1 and %d, or q.\n", len(labels))
		if attempt >= MaxTries {
			return "", giveUp()
		}
	}
}

func giveUp() error {
	return Die("giving up after %d attempts — nothing changed.", MaxTries)
}

func (c *TTYChooser) members(ctx context.Context, screen MemberScreen) (Choice, error) {
	if c.Fzf != nil {
		lines, fzfErr := c.Fzf.Choose(ctx, ui.ChooseSpec{
			Prompt: "In this group: ", Multi: true, Height: "60%", Rows: screen.Lines,
			Header: "ENTER on the first row acts on all — TAB picks specific ones, ESC goes back",
		})
		switch {
		case fzfErr == nil:
			return fzfMembers(lines)
		case errors.Is(fzfErr, ui.ErrLeave):
			return Choice{}, ErrBack
		case !errors.Is(fzfErr, ui.ErrFzfMissing):
			return Choice{}, fzfErr
		}
	}
	return c.numberedMembers(ctx, screen)
}

// fzfMembers reads the chosen lines: the all row anywhere means the whole group, else the first
// word of each line is a name.
func fzfMembers(lines []string) (Choice, error) {
	var names []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		first := fields[0]
		if first == AllRowKey {
			return Choice{All: true}, nil
		}
		names = append(names, first)
	}
	if len(names) == 0 {
		return Choice{}, ErrBack
	}
	return Choice{Names: names}, nil
}

func (c *TTYChooser) numberedMembers(ctx context.Context, screen MemberScreen) (Choice, error) {
	c.say.Printf("In this group:\n")
	for i, l := range screen.Lines {
		c.say.Printf("  %2d) %s\n", i+1, l)
	}
	for attempt := 1; ; attempt++ {
		c.say.Printf("Choice [ENTER or 1 for all, numbers for specific, b to go back]: ")
		reply, err := c.readLine(ctx)
		if errors.Is(err, io.EOF) {
			c.say.Printf("\n")
			return Choice{}, ErrBack
		}
		if err != nil {
			return Choice{}, err
		}
		switch reply {
		case "b", "B", "back", "q", "Q", "quit":
			return Choice{}, ErrBack
		case "", "1", "a", "A", "all":
			return Choice{All: true}, nil
		}
		if names, ok := ParseMemberChoice(reply, screen.Names); ok {
			return Choice{Names: names}, nil
		}
		c.say.Printf("  Not a valid choice — 1 (or ENTER) for all, 2-%d for\n  specific containers, or 'b' to go back.\n", len(screen.Names)+1)
		if attempt >= MaxTries {
			return Choice{}, giveUp()
		}
	}
}

// readLine reads one reply from the terminal with the surrounding blanks trimmed. End of input
// is io.EOF even when a partial line was typed. A cancelled context (Ctrl-C) ends the prompt's
// line and returns the cancellation, without waiting for the read.
func (c *TTYChooser) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		line, err := c.tty.ReadString('\n')
		done <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		c.say.Printf("\n")
		return "", fmt.Errorf("prompt interrupted: %w", ctx.Err())
	case r := <-done:
		if r.err != nil {
			if errors.Is(r.err, io.EOF) {
				return "", io.EOF
			}
			return "", fmt.Errorf("read reply: %w", r.err)
		}
		return strings.Trim(r.line, " \t\r\n"), nil
	}
}
