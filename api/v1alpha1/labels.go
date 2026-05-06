package v1alpha1

// Group is the API group used as the prefix for all domain labels and
// annotations.
const Group = "frp.operator.io"

// Well-known label keys.
const (
	// LabelExitPool stamps which ExitPool produced an ExitClaim. Used as
	// the dedup/idempotency key in the scheduler and for status rollup.
	LabelExitPool         = Group + "/exitpool"
	LabelProvider         = Group + "/provider"
	LabelRegion           = Group + "/region"
	LabelTier             = Group + "/tier"
	LabelCreatedForTunnel = Group + "/created-for-tunnel"
	LabelInitialized      = Group + "/initialized"
)

// Standard Karpenter requirement keys used in NodeSelectorRequirement
// values. Scheduler pins these onto claim.Spec.Requirements when an
// offering is chosen, so cloudprovider.Create can read the selected
// instance type / region directly off the claim.
const (
	RequirementInstanceType = "node.kubernetes.io/instance-type"
	RequirementRegion       = "topology.kubernetes.io/region"
	RequirementZone         = "topology.kubernetes.io/zone"
	RequirementCapacityType = "frp.operator.io/capacity-type"
)

// Well-known annotation keys.
const (
	AnnotationDoNotDisrupt      = Group + "/do-not-disrupt"
	AnnotationPoolHash          = Group + "/pool-hash"
	AnnotationProviderClassHash = Group + "/providerclass-hash"

	// ServiceWatcher annotations on Service for translation into Tunnel.Spec.
	AnnotationServiceCPURequest       = Group + "/resources.requests.cpu"
	AnnotationServiceMemoryRequest    = Group + "/resources.requests.memory"
	AnnotationServiceBandwidthRequest = Group + "/resources.requests.bandwidthMbps"
	AnnotationServiceTrafficRequest   = Group + "/resources.requests.monthlyTrafficGB"
	AnnotationServiceRequirementsJSON = Group + "/requirements"
	AnnotationServiceExitPool         = Group + "/exit-pool"
	AnnotationServiceExitClaimRef     = Group + "/exit-claim-ref"
	AnnotationServiceExpireAfter      = Group + "/expire-after"
)

// Finalizer string applied to ExitClaim. Mirrors Karpenter's single
// "karpenter.sh/termination" finalizer.
const TerminationFinalizer = Group + "/termination"

// Resource keys recognized in ResourceList fields. Standard k8s names
// (cpu, memory) need no constant. Domain-prefixed extended resources
// listed here.
const (
	ResourceExits            = Group + "/exits"
	ResourceBandwidthMbps    = Group + "/bandwidthMbps"
	ResourceMonthlyTrafficGB = Group + "/monthlyTrafficGB"
)
