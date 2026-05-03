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

// Package configuration is a minimal subset of CloudNativePG's
// internal/configuration package, ported to satisfy the dependency
// surface of pkg/certs. Only the symbols required by the cert-management
// code are present; if future syncs from CNPG need more, extend this file
// in step with that upstream package.
package configuration

const (
	// CertificateDuration is the default value for the lifetime of the generated certificates
	CertificateDuration = 90

	// ExpiringCheckThreshold is the default threshold to consider a certificate as expiring
	ExpiringCheckThreshold = 7
)

// Data is the struct containing the configuration of the operator.
// Usually the operator code will use the "Current" configuration.
type Data struct {
	// CertificateDuration is the lifetime of the generated certificates
	CertificateDuration int `json:"certificateDuration" env:"CERTIFICATE_DURATION"`

	// ExpiringCheckThreshold is the threshold to consider a certificate as expiring
	ExpiringCheckThreshold int `json:"expiringCheckThreshold" env:"EXPIRING_CHECK_THRESHOLD"`
}

// Current is the configuration used by the operator
var Current = NewConfiguration()

// NewConfiguration creates a new configuration holding the defaults
func NewConfiguration() *Data {
	return &Data{
		CertificateDuration:    CertificateDuration,
		ExpiringCheckThreshold: ExpiringCheckThreshold,
	}
}
