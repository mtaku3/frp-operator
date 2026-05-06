package v1alpha1

// Condition types used across ExitClaim, Tunnel, ExitPool, and ProviderClass.
// Mirrors Karpenter's nodeclaim_status.go condition constants.
const (
	// Lifecycle (ExitClaim).
	ConditionTypeLaunched             = "Launched"
	ConditionTypeRegistered           = "Registered"
	ConditionTypeInitialized          = "Initialized"
	ConditionTypeReady                = "Ready"
	ConditionTypeDrifted              = "Drifted"
	ConditionTypeEmpty                = "Empty"
	ConditionTypeConsolidatable       = "Consolidatable"
	ConditionTypeDisrupted            = "Disrupted"
	ConditionTypeExpired              = "Expired"
	ConditionTypeConsistentStateFound = "ConsistentStateFound"

	// Pool/ProviderClass status.
	ConditionTypeProviderClassReady  = "ProviderClassReady"
	ConditionTypeValidationSucceeded = "ValidationSucceeded"
)

// Well-known reason codes paired with conditions.
const (
	ReasonProvisioning          = "Provisioning"
	ReasonProvisioned           = "Provisioned"
	ReasonProvisioningFailed    = "ProvisioningFailed"
	ReasonRegistrationTimeout   = "RegistrationTimeout"
	ReasonAdminAPIUnreachable   = "AdminAPIUnreachable"
	ReasonPortReservationFailed = "PortReservationFailed"
	ReasonProviderError         = "ProviderError"
	ReasonProviderClassNotFound = "ProviderClassNotFound"
	ReasonLimitsExceeded        = "LimitsExceeded"
	ReasonBudgetExceeded        = "BudgetExceeded"
	ReasonNoEligibleExit        = "NoEligibleExit"
	ReasonNoMatchingPool        = "NoMatchingPool"
	ReasonPortConflict          = "PortConflict"
	ReasonInvalidRequirements   = "InvalidRequirements"
	ReasonPoolHashMismatch      = "PoolHashMismatch"
	ReasonNotReady              = "NotReady"
	ReasonReconciled            = "Reconciled"
)
