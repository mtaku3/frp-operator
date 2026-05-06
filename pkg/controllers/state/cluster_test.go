package state_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/mtaku3/frp-operator/api/v1alpha1"
	"github.com/mtaku3/frp-operator/pkg/controllers/state"
)

func newClaim(name, providerID string) *v1alpha1.ExitClaim {
	return &v1alpha1.ExitClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1alpha1.ExitClaimStatus{
			ProviderID: providerID,
			Allocatable: corev1.ResourceList{
				corev1.ResourceName("frp.operator.io/bandwidthMbps"): resource.MustParse("1000"),
			},
		},
	}
}

var _ = Describe("Cluster.UpdateExit / DeleteExit", func() {
	It("indexes by ProviderID and Name", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		Expect(c.ExitForProviderID("fake://abc")).NotTo(BeNil())
		Expect(c.ExitForName("e1")).NotTo(BeNil())

		c.DeleteExit("e1")
		Expect(c.ExitForProviderID("fake://abc")).To(BeNil())
		Expect(c.ExitForName("e1")).To(BeNil())
	})
})

var _ = Describe("Cluster pending claim index", func() {
	It("retains pre-launch claims in PendingClaims and promotes them on launch", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("p1", ""))
		Expect(c.PendingClaims()).To(HaveLen(1))
		Expect(c.PendingClaims()[0].Name).To(Equal("p1"))
		Expect(c.ExitForName("p1")).To(BeNil()) // no StateExit until launch

		// Launch.
		c.UpdateExit(newClaim("p1", "fake://p1-id"))
		Expect(c.PendingClaims()).To(BeEmpty())
		Expect(c.ExitForName("p1")).NotTo(BeNil())
	})

	It("removes pending entry on DeleteExit even before launch", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("p2", ""))
		c.DeleteExit("p2")
		Expect(c.PendingClaims()).To(BeEmpty())
	})

	It("PortsForClaimName aggregates Tunnel binding ports", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("p3", ""))
		c.UpdateTunnelBinding("default/svc-a", "p3", []int32{80, 443})
		c.UpdateTunnelBinding("default/svc-b", "p3", []int32{8080})
		ports := c.PortsForClaimName("p3")
		Expect(ports).To(HaveKey(int32(80)))
		Expect(ports).To(HaveKey(int32(443)))
		Expect(ports).To(HaveKey(int32(8080)))
		Expect(c.PortsForClaimName("nonexistent")).To(BeEmpty())
	})
})

var _ = Describe("Cluster.UpdateTunnelBinding", func() {
	It("derives StateExit.Allocations from bindings", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		c.UpdateTunnelBinding("default/svc-a", "e1", []int32{80})

		se := c.ExitForName("e1")
		Expect(se).NotTo(BeNil())
		Expect(se.UsedPorts()).To(HaveKey(int32(80)))
		Expect(se.PortHolder(80)).To(BeEquivalentTo("default/svc-a"))
	})

	It("clears allocations on unbind", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("e1", "fake://abc"))
		c.UpdateTunnelBinding("default/svc-a", "e1", []int32{80})
		c.DeleteTunnelBinding("default/svc-a")

		se := c.ExitForName("e1")
		Expect(se.IsEmpty()).To(BeTrue())
	})

	It("clears stale allocations when a tunnel moves between exits", func() {
		c := state.NewCluster(k8sClient)
		c.UpdateExit(newClaim("eA", "fake://a"))
		c.UpdateExit(newClaim("eB", "fake://b"))
		c.UpdateTunnelBinding("default/svc", "eA", []int32{80})
		c.UpdateTunnelBinding("default/svc", "eB", []int32{80})

		Expect(c.ExitForName("eA").IsEmpty()).To(BeTrue())
		Expect(c.ExitForName("eB").UsedPorts()).To(HaveKey(int32(80)))
	})
})
