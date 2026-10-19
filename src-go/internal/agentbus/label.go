package agentbus

// Label is how a team member is shown to a person, everywhere: its whole handle
// (<repo>.<seat>+<host>.<type>), never the bare seat, which two checkouts can share, and never
// cut short; the coordinator's has its role beside it. This is the one renderer of a member.
func Label(handle, role string) string {
	if role == RoleCoordinator {
		return handle + " (" + RoleCoordinator + ")"
	}
	return handle
}

// Label is the member as a person is shown it (Label).
func (m Member) Label() string { return Label(m.Handle, m.Role) }
