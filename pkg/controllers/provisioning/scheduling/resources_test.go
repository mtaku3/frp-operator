package scheduling

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourcesFit_SimpleSubtraction(t *testing.T) {
	avail := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("8Gi"),
	}
	req := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
	if !ResourcesFit(avail, req) {
		t.Fatal("should fit")
	}
}

func TestResourcesFit_OverCapacity(t *testing.T) {
	avail := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
	req := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
	if ResourcesFit(avail, req) {
		t.Fatal("should not fit")
	}
}

func TestResourcesFit_MissingDimUnbounded(t *testing.T) {
	avail := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
	req := corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("100Gi")}
	if !ResourcesFit(avail, req) {
		t.Fatal("missing dim should be unbounded")
	}
}

func TestSum(t *testing.T) {
	a := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
	b := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}
	out := Sum(a, b)
	cpu := out[corev1.ResourceCPU]
	expect := resource.MustParse("3")
	if cpu.Cmp(expect) != 0 {
		t.Fatalf("expected 3 got %s", cpu.String())
	}
}

func TestSubtract(t *testing.T) {
	a := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}
	b := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}
	out := Subtract(a, b)
	cpu := out[corev1.ResourceCPU]
	expect := resource.MustParse("3")
	if cpu.Cmp(expect) != 0 {
		t.Fatalf("expected 3 got %s", cpu.String())
	}
}
