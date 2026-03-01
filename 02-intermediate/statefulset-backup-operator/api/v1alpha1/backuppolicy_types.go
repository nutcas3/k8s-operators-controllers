package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupPolicySpec defines the desired state of BackupPolicy
type BackupPolicySpec struct {
	// Schedule in cron format
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// PVCSelector selects PVCs to backup
	// +kubebuilder:validation:Required
	PVCSelector metav1.LabelSelector `json:"pvcSelector"`

	// BackupStrategy defines how to perform backups
	// +kubebuilder:validation:Enum=snapshot;tar;custom
	// +kubebuilder:default=tar
	BackupStrategy string `json:"backupStrategy,omitempty"`

	// RetentionCount defines how many backups to keep
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=7
	RetentionCount int32 `json:"retentionCount,omitempty"`

	// BackupImage is the container image for backup jobs
	// +kubebuilder:default="busybox:latest"
	BackupImage string `json:"backupImage,omitempty"`

	// BackupStoragePVC is the PVC to store backups
	// +kubebuilder:validation:Required
	BackupStoragePVC string `json:"backupStoragePVC"`

	// Suspend pauses backup scheduling
	Suspend bool `json:"suspend,omitempty"`
}

// BackupRecord contains information about a backup
type BackupRecord struct {
	// JobName is the name of the backup job
	JobName string `json:"jobName"`

	// StartTime is when the backup started
	StartTime metav1.Time `json:"startTime"`

	// CompletionTime is when the backup completed
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Status is the backup status (Pending, Running, Succeeded, Failed)
	Status string `json:"status"`

	// Message provides additional information
	Message string `json:"message,omitempty"`
}

// BackupPolicyStatus defines the observed state of BackupPolicy
type BackupPolicyStatus struct {
	// LastScheduleTime is when the last backup was scheduled
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessfulTime is when the last backup succeeded
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`

	// BackupHistory contains recent backup information
	BackupHistory []BackupRecord `json:"backupHistory,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Suspend",type=boolean,JSONPath=`.spec.suspend`
// +kubebuilder:printcolumn:name="Last Backup",type=date,JSONPath=`.status.lastScheduleTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// BackupPolicy is the Schema for the backuppolicies API
type BackupPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupPolicySpec   `json:"spec,omitempty"`
	Status BackupPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BackupPolicyList contains a list of BackupPolicy
type BackupPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupPolicy{}, &BackupPolicyList{})
}
