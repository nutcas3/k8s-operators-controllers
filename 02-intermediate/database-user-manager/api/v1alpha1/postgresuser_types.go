package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PostgresUserSpec defines the desired state of PostgresUser
type PostgresUserSpec struct {
	// Username is the database username to create
	// +kubebuilder:validation:Required
	Username string `json:"username"`

	// Database is the database name
	// +kubebuilder:validation:Required
	Database string `json:"database"`

	// Host is the PostgreSQL server host
	// +kubebuilder:validation:Required
	Host string `json:"host"`

	// Port is the PostgreSQL server port
	// +kubebuilder:default=5432
	Port int32 `json:"port,omitempty"`

	// AdminSecretRef references the secret containing admin credentials
	// +kubebuilder:validation:Required
	AdminSecretRef corev1.SecretReference `json:"adminSecretRef"`

	// Privileges is the list of privileges to grant
	// +kubebuilder:validation:MinItems=1
	Privileges []string `json:"privileges"`

	// SecretName is the name of the secret to create with user credentials
	// +kubebuilder:validation:Required
	SecretName string `json:"secretName"`

	// RotatePassword triggers password rotation when changed
	RotatePassword bool `json:"rotatePassword,omitempty"`
}

// PostgresUserStatus defines the observed state of PostgresUser
type PostgresUserStatus struct {
	// Ready indicates if the user is ready
	Ready bool `json:"ready"`

	// Message provides additional information
	Message string `json:"message,omitempty"`

	// LastPasswordRotation is when the password was last rotated
	LastPasswordRotation *metav1.Time `json:"lastPasswordRotation,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Username",type=string,JSONPath=`.spec.username`
// +kubebuilder:printcolumn:name="Database",type=string,JSONPath=`.spec.database`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PostgresUser is the Schema for the postgresusers API
type PostgresUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PostgresUserSpec   `json:"spec,omitempty"`
	Status PostgresUserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostgresUserList contains a list of PostgresUser
type PostgresUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PostgresUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PostgresUser{}, &PostgresUserList{})
}
