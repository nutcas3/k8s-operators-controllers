package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExternalScalerSpec defines the desired state of ExternalScaler
type ExternalScalerSpec struct {
	// TargetDeployment is the name of the Deployment to scale
	// +kubebuilder:validation:Required
	TargetDeployment string `json:"targetDeployment"`

	// MetricSource is where the metric comes from: rabbitmq, redis, or http
	// +kubebuilder:validation:Enum=rabbitmq;redis;http
	MetricSource string `json:"metricSource"`

	// QueueName is the RabbitMQ queue or Redis list to measure
	QueueName string `json:"queueName,omitempty"`

	// TargetQueueDepth is the desired metric value per replica
	// +kubebuilder:validation:Minimum=1
	TargetQueueDepth int32 `json:"targetQueueDepth"`

	// MinReplicas is the lower scaling bound
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	MinReplicas int32 `json:"minReplicas"`

	// MaxReplicas is the upper scaling bound
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=10
	MaxReplicas int32 `json:"maxReplicas"`

	// RabbitmqURL is the RabbitMQ management API base URL
	RabbitmqURL string `json:"rabbitmqURL,omitempty"`

	// RabbitmqVhost is the vhost containing the queue
	// +kubebuilder:default=/
	RabbitmqVhost string `json:"rabbitmqVhost,omitempty"`

	// RedisAddress is the Redis server address (host:port)
	RedisAddress string `json:"redisAddress,omitempty"`

	// HTTPEndpoint is a URL returning JSON {"value": <number>}
	HTTPEndpoint string `json:"httpEndpoint,omitempty"`

	// CredentialsSecretRef references a secret with username/password keys
	// used to authenticate against the metric source (guest/guest if unset)
	CredentialsSecretRef *corev1.SecretReference `json:"credentialsSecretRef,omitempty"`

	// PollInterval is how often the metric is collected
	// +kubebuilder:default="30s"
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
}

// ExternalScalerStatus defines the observed state of ExternalScaler
type ExternalScalerStatus struct {
	// CurrentMetric is the last observed metric value
	CurrentMetric int32 `json:"currentMetric"`

	// CurrentReplicas is the replica count observed on the target
	CurrentReplicas int32 `json:"currentReplicas"`

	// DesiredReplicas is the replica count computed from the metric
	DesiredReplicas int32 `json:"desiredReplicas"`

	// LastScaleTime is when the target was last scaled
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`

	// Conditions represent the latest observations
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetDeployment`
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.metricSource`
// +kubebuilder:printcolumn:name="Metric",type=integer,JSONPath=`.status.currentMetric`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.status.currentReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ExternalScaler is the Schema for the externalscalers API
type ExternalScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalScalerSpec   `json:"spec,omitempty"`
	Status ExternalScalerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalScalerList contains a list of ExternalScaler
type ExternalScalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalScaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalScaler{}, &ExternalScalerList{})
}
