package proto

import "testing"

// R34 (Plan 00265): QuestionIn is the one answer to which question an event of the questions room
// is in. A question is in itself (a reminder keys on it); a post the index folded is in the
// question it was attached to, or none; a post it has not folded is in the question A 10.4's
// rules give it now; an event it cannot place is unknown.
func TestQuestionIn(t *testing.T) {
	r := newQuestionRoom(t, 0)
	q1, q2 := qid('a'), qid('b')
	r.post(q1, coordID, 9000, questionPost(1, goodQuestion, ""))
	r.post(q2, coordID, 8000, questionPost(2, goodQuestion, ""))
	r.post(qid('c'), coordID, 7000, threadPost(2, q2, q2, false))
	r.post(qid('h'), aliceID, 6000, humanPost("MIT", threadRel(q1)))
	r.post(qid('u'), aliceID, 6000, humanPost("is the build green?", ""))
	cases := []struct {
		name  string
		id    string
		h     Human
		want  int64
		known bool
	}{
		{"the question itself", q2, Human{}, 2, true},
		{"a folded post in Q1's thread", qid('h'), Human{Body: "MIT", ThreadRoot: q1}, 1, true},
		{"a folded post attached to none", qid('u'), Human{Body: "is the build green?"}, 0, true},
		{"an unfolded post in Q1's thread", qid('x'), Human{Body: "or CC-BY", ThreadRoot: q1}, 1, true},
		{"an unfolded reply to Q2's thread post", qid('y'), Human{Body: "yes", ReplyTo: qid('c')}, 2, true},
		{"an unfolded post naming Q2", qid('z'), Human{Body: "Q2: yes"}, 2, true},
		{"an unfolded post nothing places", qid('w'), Human{Body: "hello"}, 0, false},
	}
	for _, c := range cases {
		if n, known := r.index.QuestionIn(c.id, c.h); n != c.want || known != c.known {
			t.Errorf("%s: QuestionIn = Q%d known %v, want Q%d known %v", c.name, n, known, c.want, c.known)
		}
	}
}
