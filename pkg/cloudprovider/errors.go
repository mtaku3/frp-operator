package cloudprovider

import (
	"errors"
	"fmt"
)

// ExitNotFoundError signals that the cloud-side resource is gone.
// Lifecycle.Delete returns this to stop retrying once the object is
// confirmed removed.
type ExitNotFoundError struct{ ProviderID string }

func (e *ExitNotFoundError) Error() string {
	return fmt.Sprintf("exit %q not found on provider", e.ProviderID)
}

// NewExitNotFoundError constructs the error.
func NewExitNotFoundError(providerID string) error {
	return &ExitNotFoundError{ProviderID: providerID}
}

// IsExitNotFound is the Errors.As-style helper consumers use.
func IsExitNotFound(err error) bool {
	var t *ExitNotFoundError
	return errors.As(err, &t)
}

// InsufficientCapacityError signals the provider couldn't fulfill the
// claim under current quota/availability. Provisioner treats this as
// retryable.
type InsufficientCapacityError struct{ Reason string }

func (e *InsufficientCapacityError) Error() string {
	return "insufficient capacity: " + e.Reason
}

func NewInsufficientCapacityError(reason string) error {
	return &InsufficientCapacityError{Reason: reason}
}

func IsInsufficientCapacity(err error) bool {
	var t *InsufficientCapacityError
	return errors.As(err, &t)
}
