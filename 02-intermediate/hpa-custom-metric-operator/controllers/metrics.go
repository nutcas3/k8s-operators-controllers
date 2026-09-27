package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	autoscalingv1alpha1 "github.com/nutcas3/hpa-custom-metric-operator/api/v1alpha1"
	"github.com/redis/go-redis/v9"
)

// collectMetric fetches the current metric value from the configured source.
func (r *ExternalScalerReconciler) collectMetric(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler) (int32, error) {
	switch scaler.Spec.MetricSource {
	case "rabbitmq":
		return r.rabbitMQQueueDepth(ctx, scaler)
	case "redis":
		return r.redisListLength(ctx, scaler)
	case "http":
		return r.httpMetric(ctx, scaler)
	default:
		return 0, fmt.Errorf("unsupported metricSource %q", scaler.Spec.MetricSource)
	}
}

// rabbitMQQueueDepth queries the RabbitMQ management API for the number of
// messages in the configured queue.
func (r *ExternalScalerReconciler) rabbitMQQueueDepth(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler) (int32, error) {
	if scaler.Spec.RabbitmqURL == "" {
		return 0, fmt.Errorf("spec.rabbitmqURL is required for metricSource rabbitmq")
	}

	vhost := scaler.Spec.RabbitmqVhost
	if vhost == "" {
		vhost = "/"
	}

	endpoint := fmt.Sprintf("%s/api/queues/%s/%s",
		strings.TrimRight(scaler.Spec.RabbitmqURL, "/"),
		url.PathEscape(vhost),
		url.PathEscape(scaler.Spec.QueueName))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}

	username, password, err := r.metricCredentials(ctx, scaler)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(username, password)

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("rabbitmq API returned %s", resp.Status)
	}

	var queue struct {
		Messages int64 `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&queue); err != nil {
		return 0, err
	}

	return int32(queue.Messages), nil
}

// redisListLength returns LLEN of the configured list key.
func (r *ExternalScalerReconciler) redisListLength(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler) (int32, error) {
	if scaler.Spec.RedisAddress == "" {
		return 0, fmt.Errorf("spec.redisAddress is required for metricSource redis")
	}

	opts := &redis.Options{Addr: scaler.Spec.RedisAddress}
	if scaler.Spec.CredentialsSecretRef != nil {
		_, password, err := r.metricCredentials(ctx, scaler)
		if err != nil {
			return 0, err
		}
		opts.Password = password
	}

	rdb := redis.NewClient(opts)
	defer rdb.Close()

	length, err := rdb.LLen(ctx, scaler.Spec.QueueName).Result()
	if err != nil {
		return 0, err
	}

	return int32(length), nil
}

// httpMetric fetches a JSON document {"value": <number>} from an arbitrary
// endpoint, useful for custom business metrics.
func (r *ExternalScalerReconciler) httpMetric(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler) (int32, error) {
	if scaler.Spec.HTTPEndpoint == "" {
		return 0, fmt.Errorf("spec.httpEndpoint is required for metricSource http")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scaler.Spec.HTTPEndpoint, nil)
	if err != nil {
		return 0, err
	}

	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("endpoint returned %s", resp.Status)
	}

	var metric struct {
		Value int64 `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&metric); err != nil {
		return 0, err
	}

	return int32(metric.Value), nil
}

// metricCredentials resolves username/password from credentialsSecretRef,
// defaulting to RabbitMQ's guest/guest when unset.
func (r *ExternalScalerReconciler) metricCredentials(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler) (string, string, error) {
	if scaler.Spec.CredentialsSecretRef == nil {
		return "guest", "guest", nil
	}

	namespace := scaler.Spec.CredentialsSecretRef.Namespace
	if namespace == "" {
		namespace = scaler.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      scaler.Spec.CredentialsSecretRef.Name,
		Namespace: namespace,
	}, secret); err != nil {
		return "", "", fmt.Errorf("failed to get credentials secret: %w", err)
	}

	username := string(secret.Data["username"])
	if username == "" {
		username = "guest"
	}
	return username, string(secret.Data["password"]), nil
}
