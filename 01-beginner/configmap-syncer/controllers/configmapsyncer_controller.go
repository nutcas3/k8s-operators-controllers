package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/nutcas3/configmap-syncer/api/v1alpha1"
)

const (
	finalizerName = "configmapsyncer.config.example.com/finalizer"
)

// ConfigMapSyncerReconciler reconciles a ConfigMapSyncer object
type ConfigMapSyncerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=config.example.com,resources=configmapsyncers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.example.com,resources=configmapsyncers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=config.example.com,resources=configmapsyncers/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ConfigMapSyncerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 1. Fetch the ConfigMapSyncer
	syncer := &configv1alpha1.ConfigMapSyncer{}
	if err := r.Get(ctx, req.NamespacedName, syncer); err != nil {
		if errors.IsNotFound(err) {
			log.Info("ConfigMapSyncer resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ConfigMapSyncer")
		return ctrl.Result{}, err
	}

	// 2. Handle deletion with finalizers
	if !syncer.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, syncer)
	}

	// 3. Add finalizer if not present
	if !controllerutil.ContainsFinalizer(syncer, finalizerName) {
		controllerutil.AddFinalizer(syncer, finalizerName)
		if err := r.Update(ctx, syncer); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		log.Info("Added finalizer to ConfigMapSyncer")
	}

	// 4. Fetch source ConfigMap
	sourceConfigMap, err := r.getSourceConfigMap(ctx, syncer)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Source ConfigMap not found", "namespace", syncer.Spec.SourceNamespace, "name", syncer.Spec.SourceConfigMap)
			r.updateStatusCondition(ctx, syncer, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "SourceNotFound",
				Message: fmt.Sprintf("Source ConfigMap %s/%s not found", syncer.Spec.SourceNamespace, syncer.Spec.SourceConfigMap),
			})
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get source ConfigMap")
		return ctrl.Result{}, err
	}

	// 5. Sync to target namespaces
	syncedNamespaces, failedNamespaces, err := r.syncToTargets(ctx, syncer, sourceConfigMap)
	if err != nil {
		log.Error(err, "Failed to sync to targets")
		return ctrl.Result{}, err
	}

	// 6. Update status
	syncer.Status.SyncedNamespaces = syncedNamespaces
	syncer.Status.FailedNamespaces = failedNamespaces
	now := metav1.Now()
	syncer.Status.LastSyncTime = &now

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "SyncSuccessful",
		Message:            fmt.Sprintf("Successfully synced to %d namespaces", len(syncedNamespaces)),
		LastTransitionTime: now,
	}

	if len(failedNamespaces) > 0 {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "SyncPartiallyFailed"
		condition.Message = fmt.Sprintf("Synced to %d namespaces, failed: %d", len(syncedNamespaces), len(failedNamespaces))
	}

	r.updateStatusCondition(ctx, syncer, condition)

	if err := r.Status().Update(ctx, syncer); err != nil {
		log.Error(err, "Failed to update ConfigMapSyncer status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled ConfigMapSyncer",
		"synced", len(syncedNamespaces),
		"failed", len(failedNamespaces))

	return ctrl.Result{}, nil
}

// handleDeletion handles the deletion of ConfigMapSyncer with finalizer cleanup
func (r *ConfigMapSyncerReconciler) handleDeletion(ctx context.Context, syncer *configv1alpha1.ConfigMapSyncer) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(syncer, finalizerName) {
		log.Info("Cleaning up synced ConfigMaps before deletion")

		// Delete synced ConfigMaps from all target namespaces
		for _, ns := range syncer.Spec.TargetNamespaces {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      syncer.Spec.SourceConfigMap,
					Namespace: ns,
				},
			}

			if err := r.Delete(ctx, cm); err != nil {
				if !errors.IsNotFound(err) {
					log.Error(err, "Failed to delete ConfigMap", "namespace", ns, "name", syncer.Spec.SourceConfigMap)
					return ctrl.Result{}, err
				}
			} else {
				log.Info("Deleted synced ConfigMap", "namespace", ns, "name", syncer.Spec.SourceConfigMap)
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(syncer, finalizerName)
		if err := r.Update(ctx, syncer); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
		log.Info("Removed finalizer from ConfigMapSyncer")
	}

	return ctrl.Result{}, nil
}

// getSourceConfigMap fetches the source ConfigMap
func (r *ConfigMapSyncerReconciler) getSourceConfigMap(ctx context.Context, syncer *configv1alpha1.ConfigMapSyncer) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      syncer.Spec.SourceConfigMap,
		Namespace: syncer.Spec.SourceNamespace,
	}, configMap)

	return configMap, err
}

// syncToTargets syncs the source ConfigMap to all target namespaces
func (r *ConfigMapSyncerReconciler) syncToTargets(ctx context.Context, syncer *configv1alpha1.ConfigMapSyncer, source *corev1.ConfigMap) ([]string, []string, error) {
	log := log.FromContext(ctx)
	var syncedNamespaces []string
	var failedNamespaces []string

	for _, targetNS := range syncer.Spec.TargetNamespaces {
		// Check if target namespace exists
		ns := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: targetNS}, ns); err != nil {
			if errors.IsNotFound(err) {
				log.Info("Target namespace not found, skipping", "namespace", targetNS)
				failedNamespaces = append(failedNamespaces, targetNS)
				continue
			}
			log.Error(err, "Failed to check namespace", "namespace", targetNS)
			failedNamespaces = append(failedNamespaces, targetNS)
			continue
		}

		// Create target ConfigMap
		target := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      source.Name,
				Namespace: targetNS,
				Labels: map[string]string{
					"synced-by":   syncer.Name,
					"synced-from": syncer.Spec.SourceNamespace,
				},
				Annotations: map[string]string{
					"configmapsyncer.config.example.com/source-namespace": syncer.Spec.SourceNamespace,
					"configmapsyncer.config.example.com/syncer-name":      syncer.Name,
				},
			},
			Data:       source.Data,
			BinaryData: source.BinaryData,
		}

		// Check if ConfigMap already exists
		existing := &corev1.ConfigMap{}
		err := r.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: targetNS}, existing)

		if err != nil && errors.IsNotFound(err) {
			// Create new ConfigMap
			if err := r.Create(ctx, target); err != nil {
				log.Error(err, "Failed to create ConfigMap", "namespace", targetNS, "name", target.Name)
				failedNamespaces = append(failedNamespaces, targetNS)
				continue
			}
			log.Info("Created ConfigMap", "namespace", targetNS, "name", target.Name)
			syncedNamespaces = append(syncedNamespaces, targetNS)
		} else if err != nil {
			log.Error(err, "Failed to get ConfigMap", "namespace", targetNS, "name", target.Name)
			failedNamespaces = append(failedNamespaces, targetNS)
			continue
		} else {
			// Update existing ConfigMap
			existing.Data = target.Data
			existing.BinaryData = target.BinaryData
			existing.Labels = target.Labels
			existing.Annotations = target.Annotations

			if err := r.Update(ctx, existing); err != nil {
				log.Error(err, "Failed to update ConfigMap", "namespace", targetNS, "name", target.Name)
				failedNamespaces = append(failedNamespaces, targetNS)
				continue
			}
			log.Info("Updated ConfigMap", "namespace", targetNS, "name", target.Name)
			syncedNamespaces = append(syncedNamespaces, targetNS)
		}
	}

	return syncedNamespaces, failedNamespaces, nil
}

// updateStatusCondition updates or adds a condition to the status
func (r *ConfigMapSyncerReconciler) updateStatusCondition(ctx context.Context, syncer *configv1alpha1.ConfigMapSyncer, condition metav1.Condition) {
	// Find and update existing condition or append new one
	found := false
	for i, c := range syncer.Status.Conditions {
		if c.Type == condition.Type {
			syncer.Status.Conditions[i] = condition
			found = true
			break
		}
	}

	if !found {
		syncer.Status.Conditions = append(syncer.Status.Conditions, condition)
	}
}

// findSyncersForConfigMap maps ConfigMap changes to ConfigMapSyncer reconciliations
func (r *ConfigMapSyncerReconciler) findSyncersForConfigMap(ctx context.Context, cm client.Object) []reconcile.Request {
	syncers := &configv1alpha1.ConfigMapSyncerList{}
	if err := r.List(ctx, syncers); err != nil {
		return []reconcile.Request{}
	}

	var requests []reconcile.Request
	for _, syncer := range syncers.Items {
		if syncer.Spec.SourceNamespace == cm.GetNamespace() &&
			syncer.Spec.SourceConfigMap == cm.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      syncer.Name,
					Namespace: syncer.Namespace,
				},
			})
		}
	}

	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.ConfigMapSyncer{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findSyncersForConfigMap),
		).
		Complete(r)
}
