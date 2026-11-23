package client

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/stdinread"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/cli"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/records"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/wire"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	flagSkeleton = "skeleton"
	// bumpMinAgeS is bump's --min-age default: a question waiting less than an hour is not bumped,
	// so a Stop handler that bumps cannot spam the owner (DESIGN-questions-v2 2.7).
	bumpMinAgeS = 3600
)

const (
	msgBumpWhich     exits.Template = "bump takes the questions to bump, such as bump Q4 Q5, or --waiting for every waiting one; not both"
	msgSkeletonFlags exits.Template = "ask-human --skeleton prints the form and takes no other flag"
)

// skeletonText is A 10.2's question form, which ask-human --skeleton prints: the headline line,
// then the sections, each placeholder to be replaced.
const skeletonText = `<headline: the question in one line>
Context: <what the humans need to know to answer>
Question: <the question itself>
Why a human: <one of decision, physical, credential, preference> <why no agent can settle it>
Options:
1. <the first option>
2. <the second option>
`

// skeleton is ask-human --skeleton: the form on stdout, with no session and nothing sent.
func skeleton(e cli.Env, inv cli.Invocation) term.ExitCode {
	for _, f := range []string{"for", "re", "remind-after", "check", "team"} {
		if inv.Has(f) {
			return exits.Write(e.Stderr, exits.New(exits.CodeUsage, nil, msgSkeletonFlags))
		}
	}
	if _, err := io.WriteString(e.Stdout, skeletonText); err != nil {
		return exits.Write(e.Stderr, exits.New(exits.CodeInternal, nil, msgInternal, exits.Words("write its stdout")))
	}
	return term.ExitOK
}

// questionsArgs are a questions verb's args and its text (DESIGN-questions-v2 2.1-2.7): ask-human
// reads its question with the cap 8000, thread a post and retitle a headline with the cap 4000,
// each by n3f-D1's rule; close reads a note only from a stdin that is not a terminal, and an
// empty one is none; bump reads no stdin.
func questionsArgs(e cli.Env, inv cli.Invocation, op string, clk stdinread.Clock) (any, readStdinText, error) {
	switch op {
	case wire.OpAskHuman:
		text, err := readCapped(e, inv, clk, proto.MaxQuestionBody)
		a := wire.AskArgs{Team: inv.Team, For: listFlag(inv, "for"), Re: listFlag(inv, "re"), Check: inv.Has("check")}
		if s, given := inv.Flag("remind-after"); given {
			n, perr := strconv.ParseInt(s, 10, 64)
			if perr != nil {
				return nil, readStdinText{}, exits.New(exits.CodeUsage, inv.Team, msgRemindAfter)
			}
			a.RemindAfterS = &n
		}
		return a, text, err
	case wire.OpThread:
		text, err := readCapped(e, inv, clk, proto.MaxThreadBody)
		return wire.ThreadArgs{Team: inv.Team, Q: inv.Args[0], Note: inv.Has("note")}, text, err
	case wire.OpRetitle:
		text, err := readCapped(e, inv, clk, proto.MaxThreadBody)
		return wire.RetitleArgs{Team: inv.Team, Q: inv.Args[0]}, text, err
	case wire.OpClose:
		a := wire.CloseArgs{Team: inv.Team, Qs: inv.Args}
		if term.FileIsTerminal(e.Stdin) {
			return a, readStdinText{}, nil
		}
		text, err := readCapped(e, inv, clk, proto.MaxThreadBody)
		if err == nil && text.text != nil && *text.text == "" {
			text = readStdinText{}
		}
		return a, text, err
	}
	return bumpArgs(inv)
}

const msgRemindAfter exits.Template = "--remind-after takes a whole number of seconds"

// bumpArgs are bump's: the questions named or --waiting, exactly one; --min-age 3600 by default.
func bumpArgs(inv cli.Invocation) (any, readStdinText, error) {
	if (len(inv.Args) > 0) == inv.Has("waiting") {
		return nil, readStdinText{}, exits.New(exits.CodeUsage, inv.Team, msgBumpWhich)
	}
	a := wire.BumpArgs{Team: inv.Team, Qs: append([]string{}, inv.Args...), Waiting: inv.Has("waiting"), Note: optionalFlag(inv, "note"), MinAgeS: bumpMinAgeS}
	if s, given := inv.Flag("min-age"); given {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, readStdinText{}, exits.New(exits.CodeUsage, inv.Team, msgMinAge)
		}
		a.MinAgeS = n
	}
	return a, readStdinText{}, nil
}

const msgMinAge exits.Template = "--min-age takes a whole number of seconds"

// listFlag is a comma-separated flag's items, an empty list (not null) when it is not given.
func listFlag(inv cli.Invocation, name string) []string {
	v, given := inv.Flag(name)
	if !given {
		return []string{}
	}
	return strings.Split(v, ",")
}

// notRetitledWhy ends close's stderr line for a question it did not retitle.
const notRetitledWhy = " was not retitled: only the seat that asked it can edit its title, once its post is seen\n"

// notRetitled writes close's one stderr line for a question record the link says was not
// retitled (DESIGN-questions-v2 2.4); any other line writes nothing.
func notRetitled(stderr io.Writer, line []byte) error {
	n, ok := unretitled(line)
	if !ok {
		return nil
	}
	_, err := io.WriteString(stderr, "teamchat close: "+proto.QuestionName(int(n))+notRetitledWhy)
	return err
}

// unretitled is the number of a question record whose retitled is false; false for any other
// line, a line that is not a record included (it is copied as it came).
func unretitled(line []byte) (int64, bool) {
	var q struct {
		Kind     string `json:"kind"`
		N        *int64 `json:"n"`
		Retitled *bool  `json:"retitled"`
	}
	if json.Unmarshal(line, &q) != nil || q.Kind != records.KindQuestion || q.N == nil || q.Retitled == nil || *q.Retitled {
		return 0, false
	}
	return *q.N, true
}
