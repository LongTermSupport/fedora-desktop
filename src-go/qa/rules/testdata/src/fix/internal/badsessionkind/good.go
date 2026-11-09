package badsessionkind

import "fix/internal/teams/board"

func asked(r board.Row) bool { return r.Kind.IsSession() }

func aTeam(r board.Row) bool { return r.Kind == board.KindTeam }

func aWord(word string) bool { return word == "ccy" }

type kind board.Kind

// IsSession defines the question, as board.Kind.IsSession does.
func (k kind) IsSession() bool { return board.Kind(k) == board.KindCcy || board.Kind(k) == board.KindCC }
