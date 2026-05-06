package digitalocean_test

import (
	"context"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/digitalocean/godo"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider"
	"github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean"
	dov1alpha1 "github.com/mtaku3/frp-operator/pkg/cloudprovider/digitalocean/v1alpha1"
)

// stubDroplets implements the dropletAPI interface used internally.
// It's wired via SetDropletAPIFactory.
type stubDroplets struct {
	created  []godo.DropletCreateRequest
	store    map[int]*godo.Droplet
	idSeq    atomic.Int32
	listByNm map[string][]godo.Droplet
}

func newStub() *stubDroplets {
	s := &stubDroplets{store: map[int]*godo.Droplet{}, listByNm: map[string][]godo.Droplet{}}
	return s
}

func (s *stubDroplets) Create(_ context.Context, req *godo.DropletCreateRequest) (*godo.Droplet, *godo.Response, error) {
	s.created = append(s.created, *req)
	id := int(s.idSeq.Add(1))
	d := &godo.Droplet{
		ID:     id,
		Name:   req.Name,
		Region: &godo.Region{Slug: req.Region},
		Size:   &godo.Size{Slug: req.Size},
		Tags:   req.Tags,
		Networks: &godo.Networks{V4: []godo.NetworkV4{
			{Type: "public", IPAddress: "203.0.113.10"},
		}},
		Status: "active",
	}
	s.store[id] = d
	return d, &godo.Response{}, nil
}

func (s *stubDroplets) Get(_ context.Context, id int) (*godo.Droplet, *godo.Response, error) {
	d, ok := s.store[id]
	if !ok {
		return nil, &godo.Response{Response: &http.Response{StatusCode: http.StatusNotFound}}, &godo.ErrorResponse{}
	}
	return d, &godo.Response{}, nil
}

func (s *stubDroplets) Delete(_ context.Context, id int) (*godo.Response, error) {
	if _, ok := s.store[id]; !ok {
		return &godo.Response{Response: &http.Response{StatusCode: http.StatusNotFound}}, &godo.ErrorResponse{}
	}
	delete(s.store, id)
	return &godo.Response{}, nil
}

func (s *stubDroplets) ListByTag(_ context.Context, tag string, _ *godo.ListOptions) ([]godo.Droplet, *godo.Response, error) {
	var out []godo.Droplet
	for _, d := range s.store {
		if slices.Contains(d.Tags, tag) {
			out = append(out, *d)
		}
	}
	return out, &godo.Response{}, nil
}

func (s *stubDroplets) ListByName(_ context.Context, name string, _ *godo.ListOptions) ([]godo.Droplet, *godo.Response, error) {
	var out []godo.Droplet
	for _, d := range s.store {
		if d.Name == name {
			out = append(out, *d)
		}
	}
	return out, &godo.Response{}, nil
}

func TestCreate_Idempotent(t *testing.T) {
	pc := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "do-default"},
		Spec: dov1alpha1.DigitalOceanProviderClassSpec{
			Region: "nyc3",
			Size:   "s-1vcpu-1gb",
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, dov1alpha1.AddToScheme(scheme))
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	kc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pc).Build()

	cp, err := digitalocean.New(kc, "")
	require.NoError(t, err)

	stub := newStub()
	cp.SetDropletAPIFactory(func(_ context.Context, _ string) (digitalocean.DropletAPIForTest, error) {
		return stub, nil
	})

	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "exit-1"},
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "DigitalOceanProviderClass", Name: "do-default"},
			Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", BindPort: 7000, AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
		},
	}
	out, err := cp.Create(context.Background(), claim)
	require.NoError(t, err)
	require.Equal(t, "do://1", out.Status.ProviderID)
	require.Equal(t, "203.0.113.10", out.Status.PublicIP)

	// Second call must NOT create a new droplet.
	out2, err := cp.Create(context.Background(), claim)
	require.NoError(t, err)
	require.Equal(t, "do://1", out2.Status.ProviderID)
	require.Len(t, stub.created, 1, "second Create should be idempotent")
}

func TestCreate_HydratesCapacity(t *testing.T) {
	pc := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pc"},
		Spec:       dov1alpha1.DigitalOceanProviderClassSpec{Region: "nyc3", Size: "s-2vcpu-4gb"},
	}
	scheme := runtime.NewScheme()
	_ = dov1alpha1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	kc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pc).Build()
	cp, _ := digitalocean.New(kc, "")
	stub := newStub()
	cp.SetDropletAPIFactory(func(_ context.Context, _ string) (digitalocean.DropletAPIForTest, error) { return stub, nil })

	claim := &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "e"},
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "DigitalOceanProviderClass", Name: "pc"},
			Frps:             v1alpha1.FrpsConfig{Version: "v0.68.1", AllowPorts: []string{"80"}, Auth: v1alpha1.FrpsAuthConfig{Method: "token"}},
		},
	}
	out, err := cp.Create(context.Background(), claim)
	require.NoError(t, err)
	require.NotEmpty(t, out.Status.Capacity)
	require.Equal(t, "v0.68.1", out.Status.FrpsVersion)
}

func TestCreate_RefusesWrongKind(t *testing.T) {
	cp, _ := digitalocean.New(fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build(), "")
	claim := &v1alpha1.ExitClaim{
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "WrongKind"},
		},
	}
	_, err := cp.Create(context.Background(), claim)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing kind")
}

func TestDelete_NotFound(t *testing.T) {
	pc := &dov1alpha1.DigitalOceanProviderClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pc"},
		Spec:       dov1alpha1.DigitalOceanProviderClassSpec{Region: "nyc3", Size: "s-1vcpu-1gb"},
	}
	scheme := runtime.NewScheme()
	_ = dov1alpha1.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)
	kc := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pc).Build()
	cp, _ := digitalocean.New(kc, "")
	stub := newStub()
	cp.SetDropletAPIFactory(func(_ context.Context, _ string) (digitalocean.DropletAPIForTest, error) { return stub, nil })

	claim := &v1alpha1.ExitClaim{
		Spec: v1alpha1.ExitClaimSpec{
			ProviderClassRef: v1alpha1.ProviderClassRef{Kind: "DigitalOceanProviderClass", Name: "pc"},
		},
		Status: v1alpha1.ExitClaimStatus{ProviderID: "do://9999"},
	}
	err := cp.Delete(context.Background(), claim)
	require.True(t, cloudprovider.IsExitNotFound(err))
}

func TestGetSupportedProviderClasses(t *testing.T) {
	cp := &digitalocean.CloudProvider{}
	classes := cp.GetSupportedProviderClasses()
	require.Len(t, classes, 1)
	_, ok := classes[0].(*dov1alpha1.DigitalOceanProviderClass)
	require.True(t, ok)
}
