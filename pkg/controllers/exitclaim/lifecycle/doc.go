// Package lifecycle drives ExitClaim through the
// Created → Launched → Registered → Initialized → Ready phases, and
// finalizes claims on deletion (drain bound tunnels, call
// CloudProvider.Delete, strip finalizer).
package lifecycle
