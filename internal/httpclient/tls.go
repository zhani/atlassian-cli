package httpclient

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
)

// CAFileEnv names a file of extra CA certificates (PEM, one or more, or a
// single DER certificate) trusted on top of the system roots. Besides private
// CAs missing from the system store, it helps on macOS, where the platform
// verifier rejects some private server certificates (validity over 825 days,
// no subjectAltName…) whatever the keychain trusts: with extra roots set, Go
// falls back to its own verifier when the platform one fails.
const CAFileEnv = "ATLASSIAN_CA_FILE"

var (
	transportOnce sync.Once
	transport     http.RoundTripper
)

// Transport is the RoundTripper for real (non-mirror) servers: Go's default
// one (proxy from the environment etc.), plus ATLASSIAN_CA_FILE when set. If
// that file can't be used, every request fails with the reason.
func Transport() http.RoundTripper {
	transportOnce.Do(func() { transport = newTransport(os.Getenv(CAFileEnv)) })
	return transport
}

func newTransport(caFile string) http.RoundTripper {
	if caFile == "" {
		return http.DefaultTransport
	}
	pool, err := caPool(caFile)
	if err != nil {
		return errTransport{fmt.Errorf("%s: %w", CAFileEnv, err)}
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	return t
}

// caPool returns the system roots plus the certificates in file.
func caPool(file string) (*x509.CertPool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	certs, err := parseCerts(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool, nil
}

func parseCerts(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) > 0 {
		return certs, nil
	}
	// A .crt/.cer is often a single DER certificate.
	c, err := x509.ParseCertificate(data)
	if err != nil {
		return nil, errors.New("no PEM or DER certificate found")
	}
	return []*x509.Certificate{c}, nil
}

type errTransport struct{ err error }

func (e errTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }
