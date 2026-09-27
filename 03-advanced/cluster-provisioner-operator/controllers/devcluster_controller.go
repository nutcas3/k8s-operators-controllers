package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	infrav1alpha1 "github.com/nutcas3/cluster-provisioner-operator/api/v1alpha1"
)

const (
	finalizerName      = "devcluster.infrastructure.example.com/finalizer"
	healthCheckRequeue = 60 * time.Second
	retryRequeue       = 15 * time.Second
)

// DevClusterReconciler reconciles a DevCluster object
type DevClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=infrastructure.example.com,resources=devclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.example.com,resources=devclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.example.com,resources=devclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *DevClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the DevCluster
	cluster := &infrav1alpha1.DevCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	prov, err := providerFor(cluster.Spec.Provider)
	if err != nil {
		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseFailed, err.Error())
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !cluster.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, cluster, prov)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cluster, finalizerName) {
		controllerutil.AddFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	exists, err := prov.Exists(ctx, cluster.Name)
	if err != nil {
		logger.Error(err, "Failed to check cluster existence")
		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseFailed, fmt.Sprintf("Provider error: %v", err))
		return ctrl.Result{RequeueAfter: retryRequeue}, nil
	}

	if !exists {
		return r.provision(ctx, cluster, prov)
	}

	// Cluster exists: reconcile spec drift (providers recreate to change
	// version or node count)
	if cluster.Status.ObservedVersion != "" &&
		(cluster.Status.ObservedVersion != cluster.Spec.Version ||
			cluster.Status.ObservedNodes != cluster.Spec.Nodes) {
		logger.Info("Spec changed, recreating cluster",
			"version", cluster.Status.ObservedVersion+" -> "+cluster.Spec.Version,
			"nodes", fmt.Sprintf("%d -> %d", cluster.Status.ObservedNodes, cluster.Spec.Nodes))
		r.event(cluster, "Normal", "Upgrading", "Recreating cluster for spec change")

		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseUpgrading, "Recreating cluster")
		if err := prov.Delete(ctx, cluster.Name); err != nil {
			logger.Error(err, "Failed to delete cluster for upgrade")
			return ctrl.Result{RequeueAfter: retryRequeue}, err
		}
		return ctrl.Result{RequeueAfter: retryRequeue}, nil
	}

	// Ensure the kubeconfig secret exists and check health
	if err := r.ensureKubeconfigSecret(ctx, cluster, prov); err != nil {
		logger.Error(err, "Failed to ensure kubeconfig secret")
		r.updateStatus(ctx, cluster, cluster.Status.Phase, fmt.Sprintf("Kubeconfig error: %v", err))
		return ctrl.Result{RequeueAfter: retryRequeue}, nil
	}

	nodeCount, err := r.checkHealth(ctx, cluster)
	if err != nil {
		logger.Error(err, "Cluster health check failed")
		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseReady, fmt.Sprintf("Unhealthy: %v", err))
		return ctrl.Result{RequeueAfter: healthCheckRequeue}, nil
	}

	cluster.Status.NodeCount = nodeCount
	r.updateStatus(ctx, cluster, infrav1alpha1.PhaseReady, "Cluster is healthy")
	return ctrl.Result{RequeueAfter: healthCheckRequeue}, nil
}

func (r *DevClusterReconciler) provision(ctx context.Context, cluster *infrav1alpha1.DevCluster, prov clusterProvider) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	r.updateStatus(ctx, cluster, infrav1alpha1.PhaseProvisioning,
		fmt.Sprintf("Provisioning %s cluster with %d worker(s)", cluster.Spec.Provider, cluster.Spec.Nodes))
	r.event(cluster, "Normal", "Provisioning", fmt.Sprintf("Creating %s cluster %q", cluster.Spec.Provider, cluster.Name))

	logger.Info("Provisioning cluster", "name", cluster.Name, "provider", cluster.Spec.Provider)
	if err := prov.Create(ctx, cluster); err != nil {
		logger.Error(err, "Failed to create cluster")
		r.event(cluster, "Warning", "ProvisioningFailed", err.Error())
		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseFailed, fmt.Sprintf("Provisioning failed: %v", err))
		return ctrl.Result{RequeueAfter: retryRequeue}, nil
	}

	if err := r.ensureKubeconfigSecret(ctx, cluster, prov); err != nil {
		r.updateStatus(ctx, cluster, infrav1alpha1.PhaseProvisioning, fmt.Sprintf("Kubeconfig error: %v", err))
		return ctrl.Result{RequeueAfter: retryRequeue}, nil
	}

	// Record the provisioned spec so future changes trigger a recreate
	cluster.Status.ObservedVersion = cluster.Spec.Version
	cluster.Status.ObservedNodes = cluster.Spec.Nodes

	r.event(cluster, "Normal", "Provisioned", fmt.Sprintf("Cluster %q is ready", cluster.Name))
	r.updateStatus(ctx, cluster, infrav1alpha1.PhaseReady, "Cluster provisioned")
	return ctrl.Result{RequeueAfter: healthCheckRequeue}, nil
}

func (r *DevClusterReconciler) handleDeletion(ctx context.Context, cluster *infrav1alpha1.DevCluster, prov clusterProvider) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(cluster, finalizerName) {
		if exists, err := prov.Exists(ctx, cluster.Name); err != nil {
			logger.Error(err, "Failed to check cluster existence during cleanup")
		} else if exists {
			r.updateStatus(ctx, cluster, infrav1alpha1.PhaseDeleting, "Deleting cluster")
			if err := prov.Delete(ctx, cluster.Name); err != nil {
				logger.Error(err, "Failed to delete cluster")
				return ctrl.Result{RequeueAfter: retryRequeue}, err
			}
		}
		// The kubeconfig secret is garbage-collected via its owner reference

		controllerutil.RemoveFinalizer(cluster, finalizerName)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// ensureKubeconfigSecret fetches the kubeconfig from the provider and stores
// it in <name>-kubeconfig, also recording the API server endpoint in status.
func (r *DevClusterReconciler) ensureKubeconfigSecret(ctx context.Context, cluster *infrav1alpha1.DevCluster, prov clusterProvider) error {
	kubeconfig, err := prov.Kubeconfig(ctx, cluster.Name)
	if err != nil {
		return err
	}

	// Extract the API server endpoint for status
	if restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig); err == nil {
		cluster.Status.Endpoint = restConfig.Host
	}

	secretName := fmt.Sprintf("%s-kubeconfig", cluster.Name)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: cluster.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["kubeconfig"] = kubeconfig
		return controllerutil.SetControllerReference(cluster, secret, r.Scheme)
	})
	if err != nil {
		return err
	}

	cluster.Status.KubeconfigSecret = secretName
	return nil
}

// checkHealth connects to the provisioned cluster and counts its nodes.
func (r *DevClusterReconciler) checkHealth(ctx context.Context, cluster *infrav1alpha1.DevCluster) (int32, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.KubeconfigSecret,
		Namespace: cluster.Namespace,
	}, secret); err != nil {
		return 0, err
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(secret.Data["kubeconfig"])
	if err != nil {
		return 0, fmt.Errorf("invalid kubeconfig: %w", err)
	}
	restConfig.Timeout = 15 * time.Second

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return 0, err
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}

	return int32(len(nodes.Items)), nil
}

func (r *DevClusterReconciler) updateStatus(ctx context.Context, cluster *infrav1alpha1.DevCluster, phase, message string) {
	cluster.Status.Phase = phase

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             phase,
		Message:            message,
		ObservedGeneration: cluster.Generation,
		LastTransitionTime: metav1.Now(),
	}
	if phase == infrav1alpha1.PhaseReady {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ClusterReady"
	}
	cluster.Status.Conditions = []metav1.Condition{condition}

	if err := r.Status().Update(ctx, cluster); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update status")
	}
}

func (r *DevClusterReconciler) event(cluster *infrav1alpha1.DevCluster, eventtype, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Event(cluster, eventtype, reason, message)
	}
}

func (r *DevClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1alpha1.DevCluster{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
