package proto

// The drop reasons of A 7.6, a closed set: a new reason is a feature.
const (
	ReasonVersion    = "version"
	ReasonSchema     = "schema"
	ReasonSize       = "size"
	ReasonEdit       = "edit"
	ReasonSender     = "sender"
	ReasonRole       = "role"
	ReasonTarget     = "target"
	ReasonVerb       = "verb"
	ReasonRef        = "ref"
	ReasonAllowlist  = "allowlist"
	ReasonExcluded   = "excluded"
	ReasonStale      = "stale"
	ReasonRate       = "rate"
	ReasonForgeRate  = "forge-rate"
	ReasonUnresolved = "unresolved"
	ReasonProvenance = "provenance"
)

// The ignore reasons of A 7.6.
const (
	IgnoreType           = "type"
	IgnoreSeen           = "seen"
	IgnoreSelf           = "self"
	IgnoreAdmin          = "admin"
	IgnoreNotAddressed   = "not-addressed"
	IgnoreEmpty          = "empty"
	IgnoreNotCoordinator = "not-coordinator"
)

// The refusals only a send gives (A 7.6).
const (
	ReasonSecret    = "secret"
	ReasonBody      = "body"
	ReasonDuplicate = "duplicate"
)

var (
	// DropReasons is the closed set of drop reasons, in A 7.6's order.
	DropReasons = []string{
		ReasonVersion, ReasonSchema, ReasonSize, ReasonEdit, ReasonSender, ReasonRole, ReasonTarget,
		ReasonVerb, ReasonRef, ReasonAllowlist, ReasonExcluded, ReasonStale, ReasonRate,
		ReasonForgeRate, ReasonUnresolved, ReasonProvenance,
	}
	// IgnoreReasons is the closed set of ignore reasons.
	IgnoreReasons = []string{
		IgnoreType, IgnoreSeen, IgnoreSelf, IgnoreAdmin, IgnoreNotAddressed, IgnoreEmpty, IgnoreNotCoordinator,
	}
	// SendRefusals are the drop reasons and the three only a send gives.
	SendRefusals = append(append([]string(nil), DropReasons...), ReasonSecret, ReasonBody, ReasonDuplicate)
)
