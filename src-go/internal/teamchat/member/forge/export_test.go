package forge

// CacheEntries and MaxBody are the caps, for the tests that reach them.
const (
	CacheEntries = cacheEntries
	MaxBody      = maxBody
)

// SetOrigin points c at a fake GitHub for every forge kind.
func SetOrigin(c *Checker, origin string) { c.origin = func(string) string { return origin } }

// OriginOf is the origin c asks for kind.
func OriginOf(c *Checker, kind string) string { return c.origin(kind) }
