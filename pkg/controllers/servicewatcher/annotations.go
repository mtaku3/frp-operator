package servicewatcher

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
)

// ParseAnnotations reads the frp.operator.io/* annotations off a
// Service and returns the partial TunnelSpec they describe. Ports are
// translated separately by PortsFromService.
func ParseAnnotations(svc *corev1.Service) (v1alpha1.TunnelSpec, error) {
	spec := v1alpha1.TunnelSpec{}
	a := svc.Annotations

	addRequest := func(key corev1.ResourceName, raw, label string) error {
		if raw == "" {
			return nil
		}
		q, err := resource.ParseQuantity(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if spec.Resources.Requests == nil {
			spec.Resources.Requests = corev1.ResourceList{}
		}
		spec.Resources.Requests[key] = q
		return nil
	}

	if err := addRequest(corev1.ResourceCPU, a[v1alpha1.AnnotationServiceCPURequest], "cpu"); err != nil {
		return spec, err
	}
	if err := addRequest(corev1.ResourceMemory, a[v1alpha1.AnnotationServiceMemoryRequest], "memory"); err != nil {
		return spec, err
	}
	if err := addRequest(corev1.ResourceName(v1alpha1.ResourceBandwidthMbps), a[v1alpha1.AnnotationServiceBandwidthRequest], "bandwidth"); err != nil {
		return spec, err
	}
	if err := addRequest(corev1.ResourceName(v1alpha1.ResourceMonthlyTrafficGB), a[v1alpha1.AnnotationServiceTrafficRequest], "traffic"); err != nil {
		return spec, err
	}

	// Requirements: JSON-encoded array of NodeSelectorRequirementWithMinValues.
	if v := a[v1alpha1.AnnotationServiceRequirementsJSON]; v != "" {
		if err := json.Unmarshal([]byte(v), &spec.Requirements); err != nil {
			return spec, fmt.Errorf("requirements: %w", err)
		}
	}

	// Exit-pool shorthand: append a requirements entry for LabelExitPool=In{value}.
	if v := a[v1alpha1.AnnotationServiceExitPool]; v != "" {
		spec.Requirements = append(spec.Requirements, v1alpha1.NodeSelectorRequirementWithMinValues{
			Key:      v1alpha1.LabelExitPool,
			Operator: v1alpha1.NodeSelectorOpIn,
			Values:   []string{v},
		})
	}

	// Hard pin to a specific ExitClaim.
	if v := a[v1alpha1.AnnotationServiceExitClaimRef]; v != "" {
		spec.ExitClaimRef = &v1alpha1.LocalObjectReference{Name: v}
	}

	return spec, nil
}

// PortsFromService translates Service.Spec.Ports to TunnelSpec.Ports.
//
// Mapping:
//   - corev1.ServicePort.Port → TunnelPort.PublicPort (the outward face)
//   - corev1.ServicePort.TargetPort.IntValue() → TunnelPort.ServicePort,
//     defaulting to Port when TargetPort is unset / 0 / a string name.
//   - missing protocol defaults to TCP.
func PortsFromService(svc *corev1.Service) []v1alpha1.TunnelPort {
	out := make([]v1alpha1.TunnelPort, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		targetPort := p.TargetPort.IntValue()
		if targetPort == 0 {
			targetPort = int(p.Port)
		}
		protocol := string(p.Protocol)
		if protocol == "" {
			protocol = "TCP"
		}
		publicPort := p.Port
		out = append(out, v1alpha1.TunnelPort{
			Name:        p.Name,
			PublicPort:  &publicPort,
			ServicePort: int32(targetPort),
			Protocol:    protocol,
		})
	}
	return out
}
