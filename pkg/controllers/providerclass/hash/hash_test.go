package hash_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/providerclass/hash"
)

func TestSpecHash_Stable(t *testing.T) {
	pc := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "do"},
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			APITokenSecretRef:  v1alpha1.SecretKeyRef{Name: "tok", Key: "k"},
			ImageSelectorTerms: []dov1alpha1.ImageSelectorTerm{{Slug: "ubuntu-22-04-x64"}},
		},
	}
	a, err := hash.SpecHash(pc)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := hash.SpecHash(pc)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if a != b {
		t.Fatalf("non-deterministic: %s vs %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("expected 16-char hash, got %s (len=%d)", a, len(a))
	}
}

func TestSpecHash_DifferentSpecs(t *testing.T) {
	a := &dov1alpha1.DigitalOceanProviderClass{
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			ImageSelectorTerms: []dov1alpha1.ImageSelectorTerm{{Slug: "ubuntu-22-04-x64"}},
		},
	}
	b := &dov1alpha1.DigitalOceanProviderClass{
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			ImageSelectorTerms: []dov1alpha1.ImageSelectorTerm{{Slug: "ubuntu-24-04-x64"}},
		},
	}
	ha, _ := hash.SpecHash(a)
	hb, _ := hash.SpecHash(b)
	if ha == hb {
		t.Fatalf("different specs produced same hash: %s", ha)
	}
}

// TestSpecHash_ObjectMetaIgnored verifies that name/labels/annotations
// changes do not perturb the hash. Only Spec contributes.
func TestSpecHash_ObjectMetaIgnored(t *testing.T) {
	a := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "a", Annotations: map[string]string{"x": "1"}},
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			ImageSelectorTerms: []dov1alpha1.ImageSelectorTerm{{Slug: "ubuntu-22-04-x64"}},
		},
	}
	b := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "b", Annotations: map[string]string{"y": "2"}},
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			ImageSelectorTerms: []dov1alpha1.ImageSelectorTerm{{Slug: "ubuntu-22-04-x64"}},
		},
	}
	ha, _ := hash.SpecHash(a)
	hb, _ := hash.SpecHash(b)
	if ha != hb {
		t.Fatalf("metadata-only diff produced different hashes: %s vs %s", ha, hb)
	}
}
