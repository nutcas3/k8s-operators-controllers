package controllers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1alpha1 "github.com/nutcas3/hpa-custom-metric-operator/api/v1alpha1"
)

const defaultPollInterval = 30 * time.Second

// ExternalScalerReconciler reconciles a ExternalScaler object
type ExternalScalerReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	HTTPClient *http.Client
}

// +kubebuilder:rbac:groups=autoscaling.example.com,resources=externalscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling.example.com,resources=externalscalers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=autoscaling.example.com,resources=externalscalers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ExternalScalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ExternalScaler
	scaler := &autoscalingv1alpha1.ExternalScaler{}
	if err := r.Get(ctx, req.NamespacedName, scaler); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	pollInterval := defaultPollInterval
	if scaler.Spec.PollInterval != nil {
		pollInterval = scaler.Spec.PollInterval.Duration
	}

	// Fetch the target deployment
	deployment := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      scaler.Spec.TargetDeployment,
		Namespace: scaler.Namespace,
	}, deployment); err != nil {
		logger.Error(err, "Failed to get target deployment")
		r.updateStatus(ctx, scaler, 0, 0, false,
			fmt.Sprintf("Target deployment not found: %v", err))
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	currentReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		currentReplicas = *deployment.Spec.Replicas
	}

	// Collect the metric
	metric, err := r.collectMetric(ctx, scaler)
	if err != nil {
		logger.Error(err, "Failed to collect metric")
		r.updateStatus(ctx, scaler, 0, currentReplicas, false,
			fmt.Sprintf("Metric collection failed: %v", err))
		return ctrl.Result{RequeueAfter: pollInterval}, nil
	}

	// Compute desired replicas
	desired := calculateDesiredReplicas(
		metric,
		scaler.Spec.TargetQueueDepth,
		scaler.Spec.MinReplicas,
		scaler.Spec.MaxReplicas)

	// Scale the deployment if needed
	replicas := currentReplicas
	if desired != currentReplicas {
		deployment.Spec.Replicas = &desired
		if err := r.Update(ctx, deployment); err != nil {
			logger.Error(err, "Failed to scale deployment")
			r.updateStatus(ctx, scaler, metric, currentReplicas, false,
				fmt.Sprintf("Scale failed: %v", err))
			return ctrl.Result{RequeueAfter: pollInterval}, err
		}

		replicas = desired
		now := metav1.Now()
		scaler.Status.LastScaleTime = &now
		if r.Recorder != nil {
			r.Recorder.Eventf(scaler, "Normal", "Scaled",
				"Scaled %s from %d to %d replicas (metric=%d, target=%d)",
				deployment.Name, currentReplicas, desired, metric, scaler.Spec.TargetQueueDepth)
		}
		logger.Info("Scaled deployment",
			"deployment", deployment.Name,
			"from", currentReplicas, "to", desired, "metric", metric)
	}

	scaler.Status.DesiredReplicas = desired
	r.updateStatus(ctx, scaler, metric, replicas, true, "Scaling active")

	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// calculateDesiredReplicas maps a metric value to a replica count:
// ceil(metric / targetPerReplica) clamped to [min, max].
func calculateDesiredReplicas(metric, target, minReplicas, maxReplicas int32) int32 {
	if target <= 0 {
		target = 1
	}
	desired := (metric + target - 1) / target
	if desired < minReplicas {
		desired = minReplicas
	}
	if desired > maxReplicas {
		desired = maxReplicas
	}
	return desired
}

func (r *ExternalScalerReconciler) updateStatus(ctx context.Context, scaler *autoscalingv1alpha1.ExternalScaler, metric, currentReplicas int32, ready bool, message string) {
	scaler.Status.CurrentMetric = metric
	scaler.Status.CurrentReplicas = currentReplicas

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconciliationFailed",
		Message:            message,
		ObservedGeneration: scaler.Generation,
		LastTransitionTime: metav1.Now(),
	}
	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ReconciliationSucceeded"
	}
	scaler.Status.Conditions = []metav1.Condition{condition}

	if err := r.Status().Update(ctx, scaler); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update status")
	}
}

func (r *ExternalScalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1alpha1.ExternalScaler{}).
		Complete(r)
}
