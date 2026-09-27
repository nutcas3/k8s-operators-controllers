package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedApplication phases
const (
	PhasePending        = "Pending"
	PhaseMigrating      = "Migrating"
	PhaseDeploying      = "Deploying"
	PhaseHealthChecking = "HealthChecking"
	PhaseHealthy        = "Healthy"
	PhaseFailed         = "Failed"
	PhaseRolledBack     = "RolledBack"
)

// MigrationJobSpec describes a job run before the new version is deployed
type MigrationJobSpec struct {
	// Image is the migration job image
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Command is the migration entrypoint
	Command []string `json:"command,omitempty"`

	// Env is extra environment for the migration container
	Env []corev1.EnvVar `json:"env,omitempty"`
}

// CanaryStep is one traffic-weight step in a canary rollout
type CanaryStep struct {
	// Weight is the canary traffic percentage (0-100)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// Pause is how long to hold this step, in seconds
	Pause int32 `json:"pause,omitempty"`
}

// CanarySpec configures a canary rollout
type CanarySpec struct {
	// Enabled turns on canary deployment
	Enabled bool `json:"enabled"`

	// Steps is the ordered list of traffic weights
	// +kubebuilder:validation:MinItems=1
	Steps []CanaryStep `json:"steps"`
}

// UpgradeStrategy describes how to move the application to spec.version
type UpgradeStrategy struct {
	// Type is the upgrade strategy: Rolling, RollingWithMigration, or Canary
	// +kubebuilder:validation:Enum=Rolling;RollingWithMigration;Canary
	Type string `json:"type"`

	// MigrationJob runs before deployment when set (required for
	// RollingWithMigration)
	MigrationJob *MigrationJobSpec `json:"migrationJob,omitempty"`

	// HealthCheck is injected as a readiness probe on the upgraded pods and
	// gates promotion
	HealthCheck *corev1.Probe `json:"healthCheck,omitempty"`

	// Canary configures weighted canary steps (used with type=Canary)
	Canary *CanarySpec `json:"canary,omitempty"`
}

// ManagedApplicationSpec defines the desired state of ManagedApplication
type ManagedApplicationSpec struct {
	// DeploymentName is the Deployment to upgrade (defaults to the CR name)
	DeploymentName string `json:"deploymentName,omitempty"`

	// Image is the container image repository (without tag)
	// +kubebuilder:validation:Required
	Image string `json:"image"`

	// Version is the target image tag
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Paused halts all upgrade activity
	Paused bool `json:"paused,omitempty"`

	// UpgradeStrategy describes how to perform the upgrade
	// +kubebuilder:validation:Required
	UpgradeStrategy UpgradeStrategy `json:"upgradeStrategy"`
}

// ManagedApplicationStatus defines the observed state of ManagedApplication
type ManagedApplicationStatus struct {
	// Phase is the upgrade phase: Pending, Migrating, Deploying,
	// HealthChecking, Healthy, Failed, or RolledBack
	Phase string `json:"phase"`

	// CurrentVersion is the version running after the last successful upgrade
	CurrentVersion string `json:"currentVersion,omitempty"`

	// TargetVersion is the version being rolled out
	TargetVersion string `json:"targetVersion,omitempty"`

	// CanaryWeight is the current canary traffic percentage
	CanaryWeight int32 `json:"canaryWeight,omitempty"`

	// CanaryStep is the index into spec.upgradeStrategy.canary.steps
	CanaryStep int32 `json:"canaryStep,omitempty"`

	// PhaseStartedAt is when the current phase began (drives timeouts)
	PhaseStartedAt *metav1.Time `json:"phaseStartedAt,omitempty"`

	// CanaryStepStartedAt is when the current canary step began
	CanaryStepStartedAt *metav1.Time `json:"canaryStepStartedAt,omitempty"`

	// Message provides additional information
	Message string `json:"message,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.currentVersion`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.status.targetVersion`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Canary",type=integer,JSONPath=`.status.canaryWeight`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ManagedApplication is the Schema for the managedapplications API
type ManagedApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedApplicationSpec   `json:"spec,omitempty"`
	Status ManagedApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedApplicationList contains a list of ManagedApplication
type ManagedApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedApplication{}, &ManagedApplicationList{})
}
