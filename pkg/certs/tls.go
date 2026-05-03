/*
Copyright (C) 2026.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<https://www.gnu.org/licenses/agpl-3.0.html>.
*/

package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type contextKey string

// contextKeyTLSConfig is the context key holding the TLS configuration
const contextKeyTLSConfig contextKey = "tlsConfig"

// newTLSConfigFromSecret creates a tls.Config from the given CA secret.
func newTLSConfigFromSecret(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
) (*tls.Config, error) {
	secret := &corev1.Secret{}
	err := cli.Get(ctx, caSecret, secret)
	if err != nil {
		return nil, fmt.Errorf("while getting caSecret %s: %w", caSecret.Name, err)
	}

	caCertificate, ok := secret.Data[CACertKey]
	if !ok {
		return nil, fmt.Errorf("missing %s entry in secret %s", CACertKey, caSecret.Name)
	}

	// The operator will verify the certificates only against the CA, ignoring the DNS name.
	// This behavior is because user-provided certificates could not have the DNS name
	// for the <cluster>-rw service, which would cause a name verification error.
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)

	return NewTLSConfigFromCertPool(caCertPool), nil
}

// verifyCertificates validates the peer certificate chain against the trusted CA pool.
func verifyCertificates(certPool *x509.CertPool, certs []*x509.Certificate) error {
	if len(certs) == 0 {
		return fmt.Errorf("no certificates provided")
	}
	opts := x509.VerifyOptions{
		Roots:         certPool,
		Intermediates: x509.NewCertPool(),
	}
	for _, cert := range certs[1:] {
		opts.Intermediates.AddCert(cert)
	}
	_, err := certs[0].Verify(opts)
	if err != nil {
		return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
	}

	return nil
}

// NewTLSConfigFromCertPool creates a tls.Config object from X509 cert pool
// containing the expected server CA
func NewTLSConfigFromCertPool(
	certPool *x509.CertPool,
) *tls.Config {
	tlsConfig := tls.Config{
		MinVersion:         tls.VersionTLS13,
		RootCAs:            certPool,
		InsecureSkipVerify: true, //#nosec G402 -- we are verifying the certificate ourselves
		// VerifyConnection runs on every completed handshake, including resumed
		// TLS 1.3 sessions where no certificate exchange occurs but the original
		// peer certificates remain available in tls.ConnectionState.
		VerifyConnection: func(conn tls.ConnectionState) error {
			return verifyCertificates(certPool, conn.PeerCertificates)
		},
	}

	return &tlsConfig
}

// NewTLSConfigForContext creates a tls.config with the provided data and returns an expanded context that contains
// the *tls.Config
func NewTLSConfigForContext(
	ctx context.Context,
	cli client.Client,
	caSecret types.NamespacedName,
) (context.Context, error) {
	conf, err := newTLSConfigFromSecret(ctx, cli, caSecret)
	if err != nil {
		return ctx, err
	}

	return context.WithValue(ctx, contextKeyTLSConfig, conf), nil
}

// GetTLSConfigFromContext returns the *tls.Config contained by the context or any error encountered
func GetTLSConfigFromContext(ctx context.Context) (*tls.Config, error) {
	conf, ok := ctx.Value(contextKeyTLSConfig).(*tls.Config)
	if !ok || conf == nil {
		return nil, fmt.Errorf("context does not contain TLSConfig")
	}
	return conf, nil
}
