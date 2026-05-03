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

package kubernetes

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForDeleted polls until the named object Get returns a NotFound
// error, or the timeout elapses.
func WaitForDeleted(ctx context.Context, c client.Client, obj client.Object, timeout time.Duration) error {
	key := client.ObjectKeyFromObject(obj)
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("object %s not deleted within %s", key, timeout)
		}
		err := c.Get(ctx, key, obj)
		if client.IgnoreNotFound(err) == nil && err != nil {
			return nil
		}
		time.Sleep(time.Second)
	}
}
