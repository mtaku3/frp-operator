/*
Copyright (C) 2026.

Licensed under the GNU Affero General Public License, version 3.
*/

package kubernetes

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitFor polls fn until it returns (true, nil) or the deadline elapses.
// fn errors are retried; the last one is returned on timeout.
func WaitFor(ctx context.Context, fn func(context.Context) (bool, error), timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, err := fn(ctx)
		if err == nil && ok {
			return nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("waited %s: %w", timeout, lastErr)
			}
			return fmt.Errorf("condition not met within %s", timeout)
		}
		time.Sleep(interval)
	}
}

// WaitForDeleted polls until Get returns NotFound for obj.
func WaitForDeleted(ctx context.Context, c client.Client, obj client.Object, timeout time.Duration) error {
	key := client.ObjectKeyFromObject(obj)
	deadline := time.Now().Add(timeout)
	for {
		err := c.Get(ctx, key, obj)
		if err != nil && client.IgnoreNotFound(err) == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("object %s not deleted within %s", key, timeout)
		}
		time.Sleep(time.Second)
	}
}
