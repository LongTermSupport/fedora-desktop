// Package badtls builds a transport and a TLS configuration where teamchat must not: each
// construct is a way to decide, outside the one home, whom a client trusts.
package badtls

import (
	"crypto/tls"
	"net/http"
	"time"
)

// Alias is another spelling of the transport type.
type Alias = http.Transport

// Holder keeps a configuration by value.
type Holder struct {
	Cfg tls.Config
}

// Literal is the loopback probe's original: a pointer to a transport literal.
func Literal() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}} // want `http.Transport built outside package matrix`
}

// Value is a transport literal held by value.
func Value() http.Transport {
	return http.Transport{IdleConnTimeout: time.Minute} // want `http.Transport built outside package matrix`
}

// ViaNew is the builtin new.
func ViaNew() *http.Transport {
	return new(http.Transport) // want `http.Transport built outside package matrix`
}

// ViaVar is the zero value of a declared variable.
func ViaVar() *http.Transport {
	var t http.Transport // want `http.Transport built outside package matrix`
	return &t
}

// ViaAlias builds the transport under another name.
func ViaAlias() *Alias {
	return &Alias{DisableCompression: true} // want `http.Transport built outside package matrix`
}

// ViaClone copies the default transport, which holds the system's trust.
func ViaClone() *http.Transport {
	def, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil
	}
	return def.Clone() // want `http.Transport built outside package matrix`
}

// Config is a TLS configuration literal, the trust decision itself.
func Config(pool *tlsPool) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: pool.name} // want `tls.Config built outside package matrix`
}

// ConfigNew is the builtin new on a TLS configuration.
func ConfigNew() *tls.Config {
	return new(tls.Config) // want `tls.Config built outside package matrix`
}

// ConfigVar is a declared TLS configuration.
func ConfigVar() *tls.Config {
	var c tls.Config // want `tls.Config built outside package matrix`
	return &c
}

// ConfigClone copies a configuration it was given.
func ConfigClone(c *tls.Config) *tls.Config {
	return c.Clone() // want `tls.Config built outside package matrix`
}

// Nested hides the transports in a slice literal whose element type is elided.
func Nested() []http.Transport {
	return []http.Transport{{DisableKeepAlives: true}} // want `http.Transport built outside package matrix`
}

// InStruct builds the configuration in a field of a literal.
func InStruct() *Holder {
	return &Holder{Cfg: tls.Config{MinVersion: tls.VersionTLS12}} // want `tls.Config built outside package matrix`
}

type tlsPool struct{ name string }
