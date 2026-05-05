package servicewatcher_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/servicewatcher"
)

func mustQty(t *testing.T, s string) resource.Quantity {
	t.Helper()
	q, err := resource.ParseQuantity(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return q
}

func TestParseAnnotations_Empty(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "x"}}
	spec, err := servicewatcher.ParseAnnotations(svc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(spec.Resources.Requests) != 0 || len(spec.Requirements) != 0 || spec.ExitClaimRef != nil {
		t.Fatalf("expected zero spec, got %+v", spec)
	}
}

func TestParseAnnotations_Resources(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceCPURequest:       "250m",
		v1alpha1.AnnotationServiceMemoryRequest:    "128Mi",
		v1alpha1.AnnotationServiceBandwidthRequest: "100",
		v1alpha1.AnnotationServiceTrafficRequest:   "500",
	}}}
	spec, err := servicewatcher.ParseAnnotations(svc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := corev1.ResourceList{
		corev1.ResourceCPU:    mustQty(t, "250m"),
		corev1.ResourceMemory: mustQty(t, "128Mi"),
		corev1.ResourceName(v1alpha1.ResourceBandwidthMbps):    mustQty(t, "100"),
		corev1.ResourceName(v1alpha1.ResourceMonthlyTrafficGB): mustQty(t, "500"),
	}
	for k, v := range want {
		got, ok := spec.Resources.Requests[k]
		if !ok {
			t.Fatalf("missing %s", k)
		}
		if got.Cmp(v) != 0 {
			t.Fatalf("%s: want %s got %s", k, v.String(), got.String())
		}
	}
}

func TestParseAnnotations_BadCPU(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceCPURequest: "not-a-number",
	}}}
	if _, err := servicewatcher.ParseAnnotations(svc); err == nil {
		t.Fatalf("expected error for bad cpu")
	}
}

func TestParseAnnotations_RequirementsJSON(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceRequirementsJSON: `[{"key":"frp.operator.io/region","operator":"In","values":["us-east"]}]`,
	}}}
	spec, err := servicewatcher.ParseAnnotations(svc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(spec.Requirements) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(spec.Requirements))
	}
	r := spec.Requirements[0]
	if r.Key != v1alpha1.LabelRegion || r.Operator != v1alpha1.NodeSelectorOpIn || len(r.Values) != 1 || r.Values[0] != "us-east" {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseAnnotations_BadRequirementsJSON(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceRequirementsJSON: `not-json`,
	}}}
	if _, err := servicewatcher.ParseAnnotations(svc); err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseAnnotations_ExitPoolShorthand(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceExitPool: "shared",
	}}}
	spec, err := servicewatcher.ParseAnnotations(svc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(spec.Requirements) != 1 {
		t.Fatalf("want 1 requirement, got %d", len(spec.Requirements))
	}
	r := spec.Requirements[0]
	if r.Key != v1alpha1.LabelExitPool || r.Operator != v1alpha1.NodeSelectorOpIn || len(r.Values) != 1 || r.Values[0] != "shared" {
		t.Fatalf("unexpected requirement: %+v", r)
	}
}

func TestParseAnnotations_ExitClaimRef(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
		v1alpha1.AnnotationServiceExitClaimRef: "claim-xyz",
	}}}
	spec, err := servicewatcher.ParseAnnotations(svc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if spec.ExitClaimRef == nil || spec.ExitClaimRef.Name != "claim-xyz" {
		t.Fatalf("ExitClaimRef: %+v", spec.ExitClaimRef)
	}
}

func TestPortsFromService_NumericTargetPort(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Name: "http", Port: 80, TargetPort: intstr.FromInt(8080), Protocol: corev1.ProtocolTCP},
	}}}
	out := servicewatcher.PortsFromService(svc)
	if len(out) != 1 {
		t.Fatalf("want 1 port, got %d", len(out))
	}
	p := out[0]
	if p.Name != "http" || p.PublicPort == nil || *p.PublicPort != 80 || p.ServicePort != 8080 || p.Protocol != "TCP" {
		t.Fatalf("unexpected port: %+v (publicPort=%v)", p, p.PublicPort)
	}
}

func TestPortsFromService_NamedTargetPortFallsBackToPort(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Port: 443, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP},
	}}}
	out := servicewatcher.PortsFromService(svc)
	if out[0].ServicePort != 443 {
		t.Fatalf("expected fallback to Port=443, got %d", out[0].ServicePort)
	}
}

func TestPortsFromService_DefaultProtocol(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Port: 53, TargetPort: intstr.FromInt(53)},
	}}}
	out := servicewatcher.PortsFromService(svc)
	if out[0].Protocol != "TCP" {
		t.Fatalf("expected default TCP, got %q", out[0].Protocol)
	}
}

func TestPortsFromService_UDP(t *testing.T) {
	svc := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{
		{Port: 53, TargetPort: intstr.FromInt(53), Protocol: corev1.ProtocolUDP},
	}}}
	out := servicewatcher.PortsFromService(svc)
	if out[0].Protocol != "UDP" {
		t.Fatalf("expected UDP, got %q", out[0].Protocol)
	}
}
