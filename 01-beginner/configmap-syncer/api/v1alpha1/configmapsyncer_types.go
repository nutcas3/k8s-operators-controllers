package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapSyncerSpec defines the desired state of ConfigMapSyncer
type ConfigMapSyncerSpec struct {
	// SourceNamespace is the namespace containing the source ConfigMap
	// +kubebuilder:validation:Required
	SourceNamespace string `json:"sourceNamespace"`

	// SourceConfigMap is the name of the ConfigMap to sync
	// +kubebuilder:validation:Required
	SourceConfigMap string `json:"sourceConfigMap"`

	// TargetNamespaces is the list of namespaces to sync to
	// +kubebuilder:validation:MinItems=1
	TargetNamespaces []string `json:"targetNamespaces"`
}

// ConfigMapSyncerStatus defines the observed state of ConfigMapSyncer
type ConfigMapSyncerStatus struct {
	// SyncedNamespaces lists successfully synced namespaces
	SyncedNamespaces []string `json:"syncedNamespaces,omitempty"`

	// FailedNamespaces lists namespaces that failed to sync
	FailedNamespaces []string `json:"failedNamespaces,omitempty"`

	// LastSyncTime is the last successful sync timestamp
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ConfigMapSyncer is the Schema for the configmapsyncers API
type ConfigMapSyncer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigMapSyncerSpec   `json:"spec,omitempty"`
	Status ConfigMapSyncerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ConfigMapSyncerList contains a list of ConfigMapSyncer
type ConfigMapSyncerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigMapSyncer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigMapSyncer{}, &ConfigMapSyncerList{})
}
