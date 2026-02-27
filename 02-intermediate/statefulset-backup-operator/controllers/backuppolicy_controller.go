package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/robfig/cron/v3"
	backupv1alpha1 "github.com/nutcas3/statefulset-backup-operator/api/v1alpha1"
)

const (
	finalizerName = "backuppolicy.backup.example.com/finalizer"
)

// BackupPolicyReconciler reconciles a BackupPolicy object
type BackupPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=backup.example.com,resources=backuppolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=backup.example.com,resources=backuppolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=backup.example.com,resources=backuppolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch

func (r *BackupPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the BackupPolicy
	policy := &backupv1alpha1.BackupPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !policy.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, policy)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(policy, finalizerName) {
		controllerutil.AddFinalizer(policy, finalizerName)
		if err := r.Update(ctx, policy); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check if suspended
	if policy.Spec.Suspend {
		log.Info("Backup policy is suspended")
		r.updateCondition(ctx, policy, "Suspended", metav1.ConditionTrue, "PolicySuspended", "Backup policy is suspended")
		return ctrl.Result{}, nil
	}

	// Update backup history from existing jobs
	if err := r.updateBackupHistory(ctx, policy); err != nil {
		log.Error(err, "Failed to update backup history")
	}

	// Check if it's time for a backup
	nextSchedule, err := r.getNextScheduleTime(policy)
	if err != nil {
		log.Error(err, "Failed to parse schedule")
		r.updateCondition(ctx, policy, "Ready", metav1.ConditionFalse, "InvalidSchedule", fmt.Sprintf("Invalid cron schedule: %v", err))
		return ctrl.Result{}, err
	}

	now := time.Now()
	if now.Before(nextSchedule) {
		// Not time yet, requeue at next schedule time
		requeueAfter := nextSchedule.Sub(now)
		log.Info("Next backup scheduled", "after", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// Time to create a backup
	log.Info("Creating backup jobs")

	// Find PVCs to backup
	pvcs, err := r.findPVCsToBackup(ctx, policy)
	if err != nil {
		log.Error(err, "Failed to find PVCs")
		r.updateCondition(ctx, policy, "Ready", metav1.ConditionFalse, "PVCLookupFailed", fmt.Sprintf("Failed to find PVCs: %v", err))
		return ctrl.Result{}, err
	}

	if len(pvcs) == 0 {
		log.Info("No PVCs found matching selector")
		r.updateCondition(ctx, policy, "Ready", metav1.ConditionTrue, "NoPVCs", "No PVCs found matching selector")
		// Still requeue for next schedule
		nextSchedule, _ = r.getNextScheduleTime(policy)
		return ctrl.Result{RequeueAfter: time.Until(nextSchedule)}, nil
	}

	// Create backup jobs
	for _, pvc := range pvcs {
		if err := r.createBackupJob(ctx, policy, &pvc); err != nil {
			log.Error(err, "Failed to create backup job", "pvc", pvc.Name)
			r.updateCondition(ctx, policy, "Ready", metav1.ConditionFalse, "JobCreationFailed", fmt.Sprintf("Failed to create backup job: %v", err))
			return ctrl.Result{}, err
		}
	}

	// Clean up old backups
	if err := r.cleanupOldBackups(ctx, policy); err != nil {
		log.Error(err, "Failed to cleanup old backups")
	}

	// Update status
	now = time.Now()
	policy.Status.LastScheduleTime = &metav1.Time{Time: now}
	r.updateCondition(ctx, policy, "Ready", metav1.ConditionTrue, "BackupScheduled", fmt.Sprintf("Scheduled %d backup job(s)", len(pvcs)))
	if err := r.Status().Update(ctx, policy); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue for next schedule
	nextSchedule, _ = r.getNextScheduleTime(policy)
	requeueAfter := time.Until(nextSchedule)
	log.Info("Backup jobs created, next backup scheduled", "after", requeueAfter)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *BackupPolicyReconciler) handleDeletion(ctx context.Context, policy *backupv1alpha1.BackupPolicy) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(policy, finalizerName) {
		// Clean up all backup jobs
		jobList := &batchv1.JobList{}
		if err := r.List(ctx, jobList, client.InNamespace(policy.Namespace),
			client.MatchingLabels{"backup-policy": policy.Name}); err != nil {
			log.Error(err, "Failed to list jobs for cleanup")
		} else {
			for _, job := range jobList.Items {
				if err := r.Delete(ctx, &job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
					log.Error(err, "Failed to delete job", "job", job.Name)
				}
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(policy, finalizerName)
		if err := r.Update(ctx, policy); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *BackupPolicyReconciler) getNextScheduleTime(policy *backupv1alpha1.BackupPolicy) (time.Time, error) {
	schedule, err := cron.ParseStandard(policy.Spec.Schedule)
	if err != nil {
		return time.Time{}, err
	}

	var lastSchedule time.Time
	if policy.Status.LastScheduleTime != nil {
		lastSchedule = policy.Status.LastScheduleTime.Time
	} else {
		lastSchedule = policy.CreationTimestamp.Time
	}

	return schedule.Next(lastSchedule), nil
}

func (r *BackupPolicyReconciler) findPVCsToBackup(ctx context.Context, policy *backupv1alpha1.BackupPolicy) ([]corev1.PersistentVolumeClaim, error) {
	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PVCSelector)
	if err != nil {
		return nil, err
	}

	pvcList := &corev1.PersistentVolumeClaimList{}
	if err := r.List(ctx, pvcList, client.InNamespace(policy.Namespace),
		client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}

	return pvcList.Items, nil
}

func (r *BackupPolicyReconciler) createBackupJob(ctx context.Context, policy *backupv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim) error {
	timestamp := time.Now().Format("20060102-150405")
	jobName := fmt.Sprintf("backup-%s-%s", pvc.Name, timestamp)

	backupImage := policy.Spec.BackupImage
	if backupImage == "" {
		backupImage = "busybox:latest"
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: policy.Namespace,
			Labels: map[string]string{
				"backup-policy": policy.Name,
				"pvc":           pvc.Name,
				"timestamp":     timestamp,
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "backup",
							Image: backupImage,
							Command: []string{
								"/bin/sh",
								"-c",
								r.getBackupCommand(policy, pvc, timestamp),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
									ReadOnly:  true,
								},
								{
									Name:      "backup",
									MountPath: "/backup",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvc.Name,
									ReadOnly:  true,
								},
							},
						},
						{
							Name: "backup",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: policy.Spec.BackupStoragePVC,
								},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(policy, job, r.Scheme); err != nil {
		return err
	}

	return r.Create(ctx, job)
}

func (r *BackupPolicyReconciler) getBackupCommand(policy *backupv1alpha1.BackupPolicy, pvc *corev1.PersistentVolumeClaim, timestamp string) string {
	backupFile := fmt.Sprintf("/backup/%s-%s.tar.gz", pvc.Name, timestamp)

	switch policy.Spec.BackupStrategy {
	case "tar":
		return fmt.Sprintf("tar czf %s -C /data . && echo 'Backup completed: %s'", backupFile, backupFile)
	case "snapshot":
		return "echo 'Snapshot strategy not implemented' && exit 1"
	case "custom":
		return "echo 'Custom backup strategy not implemented' && exit 1"
	default:
		return fmt.Sprintf("tar czf %s -C /data . && echo 'Backup completed: %s'", backupFile, backupFile)
	}
}

func (r *BackupPolicyReconciler) updateBackupHistory(ctx context.Context, policy *backupv1alpha1.BackupPolicy) error {
	// List jobs for this policy
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(policy.Namespace),
		client.MatchingLabels{"backup-policy": policy.Name}); err != nil {
		return err
	}

	var history []backupv1alpha1.BackupRecord
	for _, job := range jobList.Items {
		record := backupv1alpha1.BackupRecord{
			JobName: job.Name,
		}

		if job.Status.StartTime != nil {
			record.StartTime = *job.Status.StartTime
		}

		if job.Status.Succeeded > 0 {
			record.Status = "Succeeded"
			record.CompletionTime = job.Status.CompletionTime
			// Update last successful time
			if policy.Status.LastSuccessfulTime == nil ||
				(job.Status.CompletionTime != nil && job.Status.CompletionTime.After(policy.Status.LastSuccessfulTime.Time)) {
				policy.Status.LastSuccessfulTime = job.Status.CompletionTime
			}
		} else if job.Status.Failed > 0 {
			record.Status = "Failed"
			record.Message = "Backup job failed"
		} else if job.Status.Active > 0 {
			record.Status = "Running"
		} else {
			record.Status = "Pending"
		}

		history = append(history, record)
	}

	// Sort by start time, most recent first
	sort.Slice(history, func(i, j int) bool {
		return history[i].StartTime.After(history[j].StartTime.Time)
	})

	// Keep only recent history (last 10)
	if len(history) > 10 {
		history = history[:10]
	}

	policy.Status.BackupHistory = history
	return nil
}

func (r *BackupPolicyReconciler) cleanupOldBackups(ctx context.Context, policy *backupv1alpha1.BackupPolicy) error {
	jobList := &batchv1.JobList{}
	if err := r.List(ctx, jobList, client.InNamespace(policy.Namespace),
		client.MatchingLabels{"backup-policy": policy.Name}); err != nil {
		return err
	}

	// Sort jobs by creation time, newest first
	sort.Slice(jobList.Items, func(i, j int) bool {
		return jobList.Items[i].CreationTimestamp.After(jobList.Items[j].CreationTimestamp.Time)
	})

	retentionCount := policy.Spec.RetentionCount
	if retentionCount == 0 {
		retentionCount = 7
	}

	// Delete jobs beyond retention count
	for i := int(retentionCount); i < len(jobList.Items); i++ {
		job := &jobList.Items[i]
		if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			return err
		}
	}

	return nil
}

func (r *BackupPolicyReconciler) updateCondition(ctx context.Context, policy *backupv1alpha1.BackupPolicy, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	// Find and update existing condition or append new one
	found := false
	for i, c := range policy.Status.Conditions {
		if c.Type == conditionType {
			policy.Status.Conditions[i] = condition
			found = true
			break
		}
	}

	if !found {
		policy.Status.Conditions = append(policy.Status.Conditions, condition)
	}
}

func (r *BackupPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&backupv1alpha1.BackupPolicy{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
