/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// adminBaseURL returns the http://<publicIP>:<adminPort> URL for the given
// exit. Empty status.publicIP means "not yet provisioned"; callers must
// not call adminBaseURL until status.publicIP is set.
//
// HTTPS is a v2 item (see spec §9). v1 ships HTTP with token auth.
func adminBaseURL(exit *frpv1alpha1.ExitServer) (string, error) {
	if exit.Status.PublicIP == "" {
		return "", fmt.Errorf("admin base URL: status.publicIP not set")
	}
	port := exit.Spec.Frps.AdminPort
	if port == 0 {
		port = 7500 // schema default; defensive
	}
	return fmt.Sprintf("http://%s:%d", exit.Status.PublicIP, port), nil
}

// adminUser is the username the operator presents to frps's webServer
// admin API. The corresponding password is the random AdminPassword in the
// per-exit Secret.
const adminUser = "admin"
