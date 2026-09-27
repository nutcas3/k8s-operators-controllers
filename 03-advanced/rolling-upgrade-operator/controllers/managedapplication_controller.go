package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/nutcas3/rolling-upgrade-operator/api/v1alpha1"
)

const (
	migrationPollInterval = 10 * time.Second
	healthPollInterval    = 15 * time.Second
	healthCheckTimeout    = 10 * time.Minute
)

// ManagedApplicationReconciler reconciles a ManagedApplication object
type ManagedApplicationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=apps.example.com,resources=managedapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.example.com,resources=managedapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.example.com,resources=managedapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *ManagedApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ManagedApplication
	app := &appsv1alpha1.ManagedApplication{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if paused
	if app.Spec.Paused {
		r.updateStatus(ctx, app, app.Status.Phase, "Paused")
		return ctrl.Result{}, nil
	}

	// Adopt a deployment already running the target version
	if app.Status.CurrentVersion == "" && app.Status.Phase == "" {
		if deployed, err := r.deployedVersion(ctx, app); err == nil && deployed == app.Spec.Version {
			app.Status.CurrentVersion = app.Spec.Version
			return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseHealthy, "Already at target version")
		}
	}

	// Nothing to do when already at the desired version
	if app.Status.CurrentVersion == app.Spec.Version && app.Status.Phase == appsv1alpha1.PhaseHealthy {
		return ctrl.Result{}, nil
	}

	// Don't retry a version that already failed and rolled back; a new
	// spec.version or a status reset is required
	if app.Status.Phase == appsv1alpha1.PhaseRolledBack && app.Status.TargetVersion == app.Spec.Version {
		return ctrl.Result{}, nil
	}

	// Execute upgrade phases
	switch app.Status.Phase {
	case "", appsv1alpha1.PhasePending, appsv1alpha1.PhaseHealthy, appsv1alpha1.PhaseRolledBack:
		return r.startUpgrade(ctx, app)
	case appsv1alpha1.PhaseMigrating:
		return r.runMigration(ctx, app)
	case appsv1alpha1.PhaseDeploying:
		return r.deployNewVersion(ctx, app)
	case appsv1alpha1.PhaseHealthChecking:
		return r.performHealthCheck(ctx, app)
	case appsv1alpha1.PhaseFailed:
		return r.rollback(ctx, app)
	}

	logger.Info("Unknown phase, resetting", "phase", app.Status.Phase)
	return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhasePending, "Unknown phase, restarting")
}

// startUpgrade records the target version and picks the first real phase:
// Migrating when a migration job is configured, otherwise Deploying.
func (r *ManagedApplicationReconciler) startUpgrade(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	strategy := app.Spec.UpgradeStrategy

	if strategy.Type == "RollingWithMigration" && strategy.MigrationJob == nil {
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed,
			"upgradeStrategy.migrationJob is required for RollingWithMigration")
	}
	if strategy.Type == "Canary" && (strategy.Canary == nil || len(strategy.Canary.Steps) == 0) {
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed,
			"upgradeStrategy.canary.steps is required for Canary")
	}

	app.Status.TargetVersion = app.Spec.Version
	app.Status.CanaryWeight = 0
	app.Status.CanaryStep = 0
	app.Status.CanaryStepStartedAt = nil

	r.event(app, "Normal", "UpgradeStarted",
		fmt.Sprintf("Upgrading %s -> %s (%s)", app.Status.CurrentVersion, app.Spec.Version, strategy.Type))

	if strategy.MigrationJob != nil {
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseMigrating, "Running database migration")
	}
	return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseDeploying, "Starting deployment")
}

// runMigration creates (or polls) the migration job for the target version.
func (r *ManagedApplicationReconciler) runMigration(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	migration := app.Spec.UpgradeStrategy.MigrationJob
	jobName := fmt.Sprintf("%s-migration-%s", app.Name, app.Status.TargetVersion)

	existing := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: app.Namespace}, existing)

	if apierrors.IsNotFound(err) {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: app.Namespace,
				Labels: map[string]string{
					"app":  app.Name,
					"type": "migration",
				},
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						RestartPolicy: corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:    "migration",
								Image:   migration.Image,
								Command: migration.Command,
								Env:     migration.Env,
							},
						},
					},
				},
			},
		}
		if err := controllerutil.SetControllerReference(app, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, job); err != nil {
			return ctrl.Result{}, err
		}
		r.event(app, "Normal", "MigrationStarted", fmt.Sprintf("Started migration job %s", jobName))
		return ctrl.Result{RequeueAfter: migrationPollInterval}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if existing.Status.Succeeded > 0 {
		r.event(app, "Normal", "MigrationSucceeded", fmt.Sprintf("Migration job %s completed", jobName))
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseDeploying, "Migration completed")
	}
	if existing.Status.Failed > 0 {
		r.event(app, "Warning", "MigrationFailed", fmt.Sprintf("Migration job %s failed", jobName))
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed, "Migration job failed")
	}

	return ctrl.Result{RequeueAfter: migrationPollInterval}, nil
}

// deployNewVersion drives either a straight rolling update or the weighted
// canary steps, depending on the strategy.
func (r *ManagedApplicationReconciler) deployNewVersion(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	strategy := app.Spec.UpgradeStrategy
	if strategy.Type == "Canary" && strategy.Canary != nil && strategy.Canary.Enabled {
		return r.deployCanary(ctx, app)
	}

	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{RequeueAfter: healthPollInterval},
			r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed, fmt.Sprintf("Target deployment: %v", err))
	}

	r.setDeploymentVersion(deployment, app)
	if err := r.Update(ctx, deployment); err != nil {
		return ctrl.Result{}, err
	}

	r.event(app, "Normal", "Deploying",
		fmt.Sprintf("Updated %s to %s", deployment.Name, r.targetImage(app)))
	return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseHealthChecking, "Deployment updated")
}

// deployCanary runs one weighted step per pause window using a separate
// <deployment>-canary Deployment whose pods share the app's service labels,
// so Service traffic splits proportionally to replica counts.
func (r *ManagedApplicationReconciler) deployCanary(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	canary := app.Spec.UpgradeStrategy.Canary
	steps := canary.Steps
	step := app.Status.CanaryStep

	if step >= int32(len(steps)) {
		return r.promoteCanary(ctx, app)
	}

	current := steps[step]

	// The final weight promotes outright
	if current.Weight >= 100 {
		return r.promoteCanary(ctx, app)
	}

	// Honor the step's pause before advancing
	if app.Status.CanaryStepStartedAt != nil {
		pause := time.Duration(current.Pause) * time.Second
		elapsed := time.Since(app.Status.CanaryStepStartedAt.Time)
		if elapsed >= pause {
			app.Status.CanaryStep++
			app.Status.CanaryStepStartedAt = nil
			return ctrl.Result{}, r.Status().Update(ctx, app)
		}
	}

	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{RequeueAfter: healthPollInterval},
			r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed, fmt.Sprintf("Target deployment: %v", err))
	}

	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	canaryReplicas := (replicas*current.Weight + 99) / 100 // ceil
	if canaryReplicas < 1 {
		canaryReplicas = 1
	}

	if err := r.ensureCanaryDeployment(ctx, app, deployment, canaryReplicas); err != nil {
		return ctrl.Result{}, err
	}

	app.Status.CanaryWeight = current.Weight
	if app.Status.CanaryStepStartedAt == nil {
		now := metav1.Now()
		app.Status.CanaryStepStartedAt = &now
		r.event(app, "Normal", "CanaryStep",
			fmt.Sprintf("Canary at %d%% (%d replicas)", current.Weight, canaryReplicas))
	}
	if err := r.Status().Update(ctx, app); err != nil {
		return ctrl.Result{}, err
	}

	pause := time.Duration(current.Pause) * time.Second
	remaining := pause - time.Since(app.Status.CanaryStepStartedAt.Time)
	if remaining < time.Second {
		remaining = time.Second
	}
	return ctrl.Result{RequeueAfter: remaining}, nil
}

// promoteCanary finishes the rollout: the primary deployment takes the new
// image and the canary deployment is removed.
func (r *ManagedApplicationReconciler) promoteCanary(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{RequeueAfter: healthPollInterval},
			r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed, fmt.Sprintf("Target deployment: %v", err))
	}

	r.setDeploymentVersion(deployment, app)
	if err := r.Update(ctx, deployment); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.deleteCanaryDeployment(ctx, app); err != nil {
		return ctrl.Result{}, err
	}

	r.event(app, "Normal", "CanaryPromoted", "Canary promoted to 100%")
	return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseHealthChecking, "Canary promoted")
}

// performHealthCheck waits for the deployment rollout to complete, failing
// on ProgressDeadlineExceeded or the overall timeout.
func (r *ManagedApplicationReconciler) performHealthCheck(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{RequeueAfter: healthPollInterval}, err
	}

	for _, c := range deployment.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Reason == "ProgressDeadlineExceeded" {
			r.event(app, "Warning", "RolloutFailed", "Deployment progress deadline exceeded")
			return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed,
				"Deployment failed: progress deadline exceeded")
		}
	}

	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	if deployment.Status.ObservedGeneration >= deployment.Generation &&
		deployment.Status.UpdatedReplicas == desired &&
		deployment.Status.ReadyReplicas >= desired {

		app.Status.CurrentVersion = app.Status.TargetVersion
		app.Status.CanaryWeight = 0
		app.Status.CanaryStep = 0
		app.Status.CanaryStepStartedAt = nil
		r.event(app, "Normal", "UpgradeComplete",
			fmt.Sprintf("Upgrade to %s complete", app.Status.CurrentVersion))
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseHealthy, "Upgrade complete")
	}

	if app.Status.PhaseStartedAt != nil &&
		time.Since(app.Status.PhaseStartedAt.Time) > healthCheckTimeout {
		r.event(app, "Warning", "HealthCheckTimeout", "Deployment did not become healthy in time")
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseFailed,
			"Health check timed out")
	}

	return ctrl.Result{RequeueAfter: healthPollInterval}, nil
}

// rollback restores the previous version on the primary deployment and
// removes any canary deployment.
func (r *ManagedApplicationReconciler) rollback(ctx context.Context, app *appsv1alpha1.ManagedApplication) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if err := r.deleteCanaryDeployment(ctx, app); err != nil {
		logger.Error(err, "Failed to delete canary deployment during rollback")
	}

	if app.Status.CurrentVersion == "" {
		return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseRolledBack,
			"Upgrade failed; no previous version to restore")
	}

	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return ctrl.Result{RequeueAfter: retryRequeue}, err
	}

	previous := fmt.Sprintf("%s:%s", app.Spec.Image, app.Status.CurrentVersion)
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		deployment.Spec.Template.Spec.Containers[0].Image = previous
		if err := r.Update(ctx, deployment); err != nil {
			return ctrl.Result{RequeueAfter: retryRequeue}, err
		}
	}

	r.event(app, "Warning", "RolledBack",
		fmt.Sprintf("Rolled back to %s after failed upgrade to %s",
			app.Status.CurrentVersion, app.Status.TargetVersion))
	return ctrl.Result{}, r.transitionTo(ctx, app, appsv1alpha1.PhaseRolledBack,
		fmt.Sprintf("Rolled back to %s", app.Status.CurrentVersion))
}

const retryRequeue = 15 * time.Second

func (r *ManagedApplicationReconciler) deploymentName(app *appsv1alpha1.ManagedApplication) string {
	if app.Spec.DeploymentName != "" {
		return app.Spec.DeploymentName
	}
	return app.Name
}

func (r *ManagedApplicationReconciler) targetDeployment(ctx context.Context, app *appsv1alpha1.ManagedApplication) (*appsv1.Deployment, error) {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.deploymentName(app),
		Namespace: app.Namespace,
	}, deployment)
	return deployment, err
}

func (r *ManagedApplicationReconciler) targetImage(app *appsv1alpha1.ManagedApplication) string {
	return fmt.Sprintf("%s:%s", app.Spec.Image, app.Status.TargetVersion)
}

// deployedVersion reads the image tag of the first container of the target
// deployment, used to adopt already-upgraded workloads.
func (r *ManagedApplicationReconciler) deployedVersion(ctx context.Context, app *appsv1alpha1.ManagedApplication) (string, error) {
	deployment, err := r.targetDeployment(ctx, app)
	if err != nil {
		return "", err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "", fmt.Errorf("deployment has no containers")
	}
	image := deployment.Spec.Template.Spec.Containers[0].Image
	prefix := app.Spec.Image + ":"
	if len(image) > len(prefix) && image[:len(prefix)] == prefix {
		return image[len(prefix):], nil
	}
	return "", fmt.Errorf("image %q does not match %s:*", image, app.Spec.Image)
}

// setDeploymentVersion points the deployment's first container at the target
// image and injects the configured health check as a readiness probe.
func (r *ManagedApplicationReconciler) setDeploymentVersion(deployment *appsv1.Deployment, app *appsv1alpha1.ManagedApplication) {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return
	}
	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Image = r.targetImage(app)
	if probe := app.Spec.UpgradeStrategy.HealthCheck; probe != nil {
		container.ReadinessProbe = probe.DeepCopy()
	}
}

func (r *ManagedApplicationReconciler) canaryName(app *appsv1alpha1.ManagedApplication) string {
	return fmt.Sprintf("%s-canary", r.deploymentName(app))
}

// ensureCanaryDeployment creates or updates the canary deployment: a copy of
// the primary pod template running the target image at canaryReplicas.
func (r *ManagedApplicationReconciler) ensureCanaryDeployment(ctx context.Context, app *appsv1alpha1.ManagedApplication, primary *appsv1.Deployment, replicas int32) error {
	canary := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.canaryName(app),
			Namespace: app.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, canary, func() error {
		canary.Labels = map[string]string{
			"app":   app.Name,
			"type":  "canary",
			"track": "canary",
		}
		canary.Spec.Replicas = &replicas
		canary.Spec.Selector = primary.Spec.Selector.DeepCopy()
		if canary.Spec.Selector == nil {
			canary.Spec.Selector = &metav1.LabelSelector{}
		}
		if canary.Spec.Selector.MatchLabels == nil {
			canary.Spec.Selector.MatchLabels = map[string]string{}
		}
		canary.Spec.Selector.MatchLabels["track"] = "canary"

		canary.Spec.Template = *primary.Spec.Template.DeepCopy()
		if canary.Spec.Template.Labels == nil {
			canary.Spec.Template.Labels = map[string]string{}
		}
		canary.Spec.Template.Labels["track"] = "canary"
		for k, v := range canary.Spec.Selector.MatchLabels {
			canary.Spec.Template.Labels[k] = v
		}

		if len(canary.Spec.Template.Spec.Containers) > 0 {
			container := &canary.Spec.Template.Spec.Containers[0]
			container.Image = r.targetImage(app)
			if probe := app.Spec.UpgradeStrategy.HealthCheck; probe != nil {
				container.ReadinessProbe = probe.DeepCopy()
			}
		}

		return controllerutil.SetControllerReference(app, canary, r.Scheme)
	})
	return err
}

func (r *ManagedApplicationReconciler) deleteCanaryDeployment(ctx context.Context, app *appsv1alpha1.ManagedApplication) error {
	canary := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.canaryName(app),
			Namespace: app.Namespace,
		},
	}
	err := r.Delete(ctx, canary)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// transitionTo records a phase change and persists the status in one write.
func (r *ManagedApplicationReconciler) transitionTo(ctx context.Context, app *appsv1alpha1.ManagedApplication, phase, message string) error {
	now := metav1.Now()
	app.Status.Phase = phase
	app.Status.PhaseStartedAt = &now
	r.updateStatusFields(app, phase, message)
	return r.Status().Update(ctx, app)
}

// updateStatus refreshes conditions without changing the phase.
func (r *ManagedApplicationReconciler) updateStatus(ctx context.Context, app *appsv1alpha1.ManagedApplication, phase, message string) {
	app.Status.Phase = phase
	r.updateStatusFields(app, phase, message)
	if err := r.Status().Update(ctx, app); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update status")
	}
}

func (r *ManagedApplicationReconciler) updateStatusFields(app *appsv1alpha1.ManagedApplication, phase, message string) {
	app.Status.Message = message

	status := metav1.ConditionFalse
	reason := phase
	if phase == appsv1alpha1.PhaseHealthy {
		status = metav1.ConditionTrue
		reason = "UpgradeComplete"
	}
	app.Status.Conditions = []metav1.Condition{{
		Type:               "Ready",
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: app.Generation,
		LastTransitionTime: metav1.Now(),
	}}
}

func (r *ManagedApplicationReconciler) event(app *appsv1alpha1.ManagedApplication, eventtype, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(app, eventtype, reason, message)
	}
}

func (r *ManagedApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.ManagedApplication{}).
		Owns(&appsv1.Deployment{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
