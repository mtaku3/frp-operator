package controller

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	frpv1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// loadBalancerClassName is the class string the operator matches.
const loadBalancerClassName = "frp-operator.io/frp"

// Annotation keys for Service → Tunnel translation. See spec §6.2.
const (
	annExit               = "frp-operator.io/exit"
	annProvider           = "frp-operator.io/provider"
	annRegion             = "frp-operator.io/region"
	annSize               = "frp-operator.io/size"
	annSchedulingPolicy   = "frp-operator.io/scheduling-policy"
	annAllowPortSplit     = "frp-operator.io/allow-port-split"
	annMigrationPolicy    = "frp-operator.io/migration-policy"
	annTrafficGB          = "frp-operator.io/traffic-gb"
	annBandwidthMbps      = "frp-operator.io/bandwidth-mbps"
	annImmutableWhenReady = "frp-operator.io/immutable-when-ready"
)

// serviceMatchesClass reports whether the Service is one this operator
// owns: type=LoadBalancer with the right loadBalancerClass.
func serviceMatchesClass(svc *corev1.Service) bool {
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return false
	}
	if svc.Spec.LoadBalancerClass == nil {
		return false
	}
	return *svc.Spec.LoadBalancerClass == loadBalancerClassName
}

// translateServiceToTunnelSpec is a pure function: Service in, TunnelSpec
// out. Returns an error only on syntactically invalid annotations.
func translateServiceToTunnelSpec(svc *corev1.Service) (frpv1alpha1.TunnelSpec, error) {
	spec := frpv1alpha1.TunnelSpec{
		Service: frpv1alpha1.ServiceRef{Name: svc.Name, Namespace: svc.Namespace},
	}
	for _, p := range svc.Spec.Ports {
		proto := frpv1alpha1.ProtocolTCP
		if p.Protocol == corev1.ProtocolUDP {
			proto = frpv1alpha1.ProtocolUDP
		}
		spec.Ports = append(spec.Ports, frpv1alpha1.TunnelPort{
			Name:        p.Name,
			ServicePort: p.Port,
			PublicPort:  ptr.To(p.Port), // public == service port unless overridden in future
			Protocol:    proto,
		})
	}

	a := svc.Annotations
	if v := a[annExit]; v != "" {
		spec.ExitRef = &frpv1alpha1.ExitRef{Name: v}
	}
	placement := &frpv1alpha1.Placement{}
	placementSet := false
	if v := a[annProvider]; v != "" {
		placement.Providers = append(placement.Providers, frpv1alpha1.Provider(v))
		placementSet = true
	}
	if v := a[annRegion]; v != "" {
		for _, r := range strings.Split(v, ",") {
			r = strings.TrimSpace(r)
			if r != "" {
				placement.Regions = append(placement.Regions, r)
			}
		}
		placementSet = true
	}
	if v := a[annSize]; v != "" {
		placement.SizeOverride = v
		placementSet = true
	}
	if placementSet {
		spec.Placement = placement
	}
	if v := a[annSchedulingPolicy]; v != "" {
		spec.SchedulingPolicyRef = frpv1alpha1.PolicyRef{Name: v}
	}
	if v := a[annAllowPortSplit]; v == "true" {
		spec.AllowPortSplit = true
	}
	if v := a[annMigrationPolicy]; v != "" {
		spec.MigrationPolicy = frpv1alpha1.MigrationPolicy(v)
	}
	if v := a[annImmutableWhenReady]; v == "true" {
		spec.ImmutableWhenReady = true
	}

	var reqSet bool
	req := &frpv1alpha1.TunnelRequirements{}
	if v := a[annTrafficGB]; v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return frpv1alpha1.TunnelSpec{}, fmt.Errorf("annotation %s=%q: %w", annTrafficGB, v, err)
		}
		req.MonthlyTrafficGB = ptr.To(n)
		reqSet = true
	}
	if v := a[annBandwidthMbps]; v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return frpv1alpha1.TunnelSpec{}, fmt.Errorf("annotation %s=%q: %w", annBandwidthMbps, v, err)
		}
		req.BandwidthMbps = ptr.To(int32(n))
		reqSet = true
	}
	if reqSet {
		spec.Requirements = req
	}

	return spec, nil
}
