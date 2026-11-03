package badtls

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
)

// Pass uses a configuration it was handed: reading or passing one on builds none.
func Pass(cfg *tls.Config) uint16 {
	return cfg.MinVersion
}

// Listen wraps a listener in a configuration the caller built.
func Listen(ln net.Listener, cfg *tls.Config) net.Listener {
	return tls.NewListener(ln, cfg)
}

// Pool is a certificate pool, not a configuration.
func Pool() *x509.CertPool {
	return x509.NewCertPool()
}

// Client takes the transport it is given.
func Client(rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt}
}

// Pointer declares a nil transport pointer, which builds nothing.
func Pointer() *http.Transport {
	var t *http.Transport
	return t
}

// Cert is a key pair, which is not a configuration.
func Cert(certPEM, keyPEM []byte) (tls.Certificate, error) {
	return tls.X509KeyPair(certPEM, keyPEM)
}
