package pki

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
)

// Fingerprint is the SHA-256 of the CA certificate's DER in caPEM, in lower-case hex (P8). It is
// what a person compares when they are told which CA a team is pinned to.
func Fingerprint(caPEM []byte) (string, error) {
	cert, err := parseCACertificate(caPEM)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:]), nil
}

// Pool is the trust a member client has: exactly the one CA certificate in caPEM and nothing
// else, built from an empty pool and never from the system's (P9).
func Pool(caPEM []byte) (*x509.CertPool, error) {
	cert, err := parseCACertificate(caPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool, nil
}
