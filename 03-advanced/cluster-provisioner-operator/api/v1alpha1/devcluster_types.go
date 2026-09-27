package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DevCluster phases
const (
	PhasePending      = "Pending"
	PhaseProvisioning = "Provisioning"
	PhaseUpgrading    = "Upgrading"
	PhaseReady        = "Ready"
	PhaseFailed       = "Failed"
	PhaseDeleting     = "Deleting"
)

// NetworkingConfig holds cluster network settings
type NetworkingConfig struct {
	// PodSubnet is the pod CIDR (e.g. 10.244.0.0/16)
	PodSubnet string `json:"podSubnet,omitempty"`

	// ServiceSubnet is the service CIDR (e.g. 10.96.0.0/12)
	ServiceSubnet string `json:"serviceSubnet,omitempty"`
}

// ClusterConfig holds optional provider-independent cluster settings
type ClusterConfig struct {
	// Networking configures pod/service subnets
	Networking NetworkingConfig `json:"networking,omitempty"`
}

// DevClusterSpec defines the desired state of DevCluster
type DevClusterSpec struct {
	// Version is the Kubernetes version (e.g. v1.28.0)
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// Nodes is the number of worker nodes (control plane not included)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	Nodes int32 `json:"nodes"`

	// Provider is the cluster provisioner: kind or k3s (via k3d)
	// +kubebuilder:validation:Enum=kind;k3s
	Provider string `json:"provider"`

	// Config holds optional cluster configuration
	Config ClusterConfig `json:"config,omitempty"`
}

// DevClusterStatus defines the observed state of DevCluster
type DevClusterStatus struct {
	// Phase is the lifecycle phase: Pending, Provisioning, Upgrading,
	// Ready, Failed, or Deleting
	Phase string `json:"phase"`

	// Endpoint is the cluster API server URL
	Endpoint string `json:"endpoint,omitempty"`

	// KubeconfigSecret is the name of the Secret holding the kubeconfig
	KubeconfigSecret string `json:"kubeconfigSecret,omitempty"`

	// NodeCount is the number of nodes reported by the cluster
	NodeCount int32 `json:"nodeCount,omitempty"`

	// ObservedVersion/Nodes record the spec last provisioned, so spec
	// changes can be detected (providers recreate to upgrade)
	ObservedVersion string `json:"observedVersion,omitempty"`
	ObservedNodes   int32  `json:"observedNodes,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=`.status.nodeCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// DevCluster is the Schema for the devclusters API
type DevCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DevClusterSpec   `json:"spec,omitempty"`
	Status DevClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DevClusterList contains a list of DevCluster
type DevClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DevCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DevCluster{}, &DevClusterList{})
}
