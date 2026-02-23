package webserver

import (
	"crypto/tls"
	"fmt"
	"sync"
)

// BuildTLSConfig returns a *tls.Config that selects the correct certificate
// for each incoming TLS connection using SNI (Server Name Indication).
// Each vhost with a non-empty TLS.Cert/Key pair is registered; connections
// to unlisted hostnames fall back to the first available certificate.
//
// Certificates are loaded once at startup and cached. Call RefreshCerts on
// the returned *certStore to reload after Let's Encrypt renewal.
func BuildTLSConfig(mounts []Mount) (*tls.Config, *CertStore, error) {
	store := &CertStore{
		certs: make(map[string]*tls.Certificate),
	}

	for _, m := range mounts {
		if !m.HasTLS {
			continue
		}
		cert, err := tls.LoadX509KeyPair(m.VHost.TLS.Cert, m.VHost.TLS.Key)
		if err != nil {
			return nil, nil, fmt.Errorf("webserver: load cert for vhost %q (%s): %w",
				m.VHost.Name, m.VHost.Host, err)
		}
		for _, host := range m.Hosts {
			store.set(host, &cert)
		}
	}

	if store.empty() {
		// No TLS vhosts configured; caller should not start TLS listener.
		return nil, store, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// GetCertificate is called for every TLS handshake. It selects the
		// certificate matching the client's SNI server name.
		GetCertificate: store.GetCertificate,
	}

	return tlsCfg, store, nil
}

// CertStore holds the per-hostname certificate map and provides thread-safe
// access for hot-reload scenarios (e.g. Let's Encrypt renewal).
type CertStore struct {
	mu    sync.RWMutex
	certs map[string]*tls.Certificate
}

func (s *CertStore) set(host string, cert *tls.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certs[host] = cert
}

func (s *CertStore) empty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.certs) == 0
}

// GetCertificate satisfies the tls.Config.GetCertificate callback signature.
// It returns the certificate registered for hello.ServerName, or the first
// available certificate as a fallback (mimics Apache's default-ssl vhost).
func (s *CertStore) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if cert, ok := s.certs[hello.ServerName]; ok {
		return cert, nil
	}
	// Fallback: return any cert (browser will still validate SNI name).
	for _, cert := range s.certs {
		return cert, nil
	}
	return nil, fmt.Errorf("webserver: no certificate available for %q", hello.ServerName)
}

// Reload reloads all certificates from disk. Call this after Let's Encrypt
// renewal (e.g. via SIGHUP or a periodic goroutine) without restarting the server.
func (s *CertStore) Reload(mounts []Mount) error {
	for _, m := range mounts {
		if !m.HasTLS {
			continue
		}
		cert, err := tls.LoadX509KeyPair(m.VHost.TLS.Cert, m.VHost.TLS.Key)
		if err != nil {
			return fmt.Errorf("webserver: reload cert for vhost %q: %w", m.VHost.Name, err)
		}
		for _, host := range m.Hosts {
			s.set(host, &cert)
		}
	}
	return nil
}
