// Package proto is the agent team bus wire protocol, version 1 (Plan 00209 SPEC-proto.md, the
// authority docs/agent-bus-protocol.md): the identifier and reference grammars, handle
// building, roles and verbs, the ping and agent-text contents, secret shapes, the receive
// steps and their verdicts, the team record and the side rooms' state, reports, questions and
// the question index, the limits as pure functions of a given time, the item lines, and the
// delivery envelope.
//
// It is pure: no I/O, no clock and no package state written after init. Its inputs are values
// as internal/pingbus/pyjson decodes them, so a string may hold a lone surrogate and an int
// stays apart from a float and a bool, as in the Python it replaces. The shared corpus
// tests/fixtures/agent-bus-protocol/ is walked against this package (corpus_test.go) and
// against the Python (tests/helpers/pingbus/test_protocol_corpus.py).
package proto
