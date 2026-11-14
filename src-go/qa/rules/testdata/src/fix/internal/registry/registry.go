// Package registry stands in for internal/registry: a session's record and the two builders of
// the arguments a recorded session is started with.
package registry

// Record is a recorded session.
type Record struct {
	Name, Prefix, Compose string
	Args                  []string
	FreshIfNoConversation bool
}

// ResumeArgs is the arguments that resume the conversation id.
func ResumeArgs(compose, prefix string, args []string, id string) ([]string, error) {
	out, err := FreshArgs(compose, prefix, args)
	return append(out, "--resume", id), err
}

// FreshArgs is the arguments that start a new conversation.
func FreshArgs(compose, prefix string, args []string) ([]string, error) {
	return append([]string{prefix, compose}, args...), nil
}
