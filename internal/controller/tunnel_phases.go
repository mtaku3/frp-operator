package controller

import (
	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// nextTunnelPhase computes the next TunnelPhase from observed conditions.
// Pure function; no side effects.
//
// Inputs:
//
//	current      = current Tunnel.status.phase
//	exitAssigned = is there an assignedExit?
//	exitReady    = is the assigned exit in PhaseReady?
//	frpcReady    = is the frpc Deployment Ready (replicas == readyReplicas > 0)?
//
// Lattice (worst-to-best):
//
//	Failed > Disconnected > Pending > Allocating > Provisioning > Connecting > Ready
//
// We only ever transition forward to Ready. Disconnected is a regression
// from Ready when frpc loses connection (frpc Deployment unhealthy).
func nextTunnelPhase(current frpv1alpha1.TunnelPhase, exitAssigned, exitReady, frpcReady bool) frpv1alpha1.TunnelPhase {
	if !exitAssigned {
		return frpv1alpha1.TunnelAllocating
	}
	if !exitReady {
		return frpv1alpha1.TunnelProvisioning
	}
	if !frpcReady {
		// Exit ready, frpc not ready: bootstrapping or disconnected.
		if current == frpv1alpha1.TunnelReady {
			return frpv1alpha1.TunnelDisconnected
		}
		return frpv1alpha1.TunnelConnecting
	}
	return frpv1alpha1.TunnelReady
}
