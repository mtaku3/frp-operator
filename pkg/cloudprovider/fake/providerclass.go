// Package fake provides an in-memory CloudProvider used by every
// controller test in pkg/controllers/. Mirrors Karpenter's
// pkg/cloudprovider/fake.
package fake

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// FakeProviderClass is a stand-in CRD used in tests. The real localdocker
// and digitalocean providers ship their own CRDs.
//
// +kubebuilder:object:generate=true
type FakeProviderClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

// DeepCopyInto copies metadata into out. Hand-written to avoid pulling in
// a generated deepcopy step for this test-only type.
func (in *FakeProviderClass) DeepCopyInto(out *FakeProviderClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
}

// DeepCopy returns a deep copy.
func (in *FakeProviderClass) DeepCopy() *FakeProviderClass {
	if in == nil {
		return nil
	}
	out := new(FakeProviderClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject implements runtime.Object.
func (in *FakeProviderClass) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}
