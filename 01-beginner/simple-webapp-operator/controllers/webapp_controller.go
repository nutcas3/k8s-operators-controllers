package controllers

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1alpha1 "github.com/nutcas3/simple-webapp-operator/api/v1alpha1"
)

// WebAppReconciler reconciles a WebApp object
type WebAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.example.com,resources=webapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.example.com,resources=webapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.example.com,resources=webapps/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the WebApp resource
	webapp := &appsv1alpha1.WebApp{}
	if err := r.Get(ctx, req.NamespacedName, webapp); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reconcile Deployment
	if err := r.reconcileDeployment(ctx, webapp); err != nil {
		log.Error(err, "Failed to reconcile Deployment")
		r.updateCondition(webapp, "Ready", metav1.ConditionFalse, "DeploymentFailed", err.Error())
		r.Status().Update(ctx, webapp)
		return ctrl.Result{}, err
	}

	// Reconcile Service
	if err := r.reconcileService(ctx, webapp); err != nil {
		log.Error(err, "Failed to reconcile Service")
		r.updateCondition(webapp, "Ready", metav1.ConditionFalse, "ServiceFailed", err.Error())
		r.Status().Update(ctx, webapp)
		return ctrl.Result{}, err
	}

	// Update Status
	if err := r.updateStatus(ctx, webapp); err != nil {
		log.Error(err, "Failed to update status")
		return ctrl.Result{}, err
	}

	log.Info("Successfully reconciled WebApp")
	return ctrl.Result{}, nil
}

func (r *WebAppReconciler) reconcileDeployment(ctx context.Context, webapp *appsv1alpha1.WebApp) error {
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      webapp.Name,
		Namespace: webapp.Namespace,
	}, deployment)

	if err != nil && errors.IsNotFound(err) {
		// Deployment doesn't exist, create it
		deployment = r.createDeployment(webapp)
		if err := controllerutil.SetControllerReference(webapp, deployment, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, deployment)
	} else if err != nil {
		return err
	}

	// Deployment exists, update if needed
	desiredDeployment := r.createDeployment(webapp)
	if !reflect.DeepEqual(deployment.Spec.Replicas, desiredDeployment.Spec.Replicas) ||
		!reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Image, desiredDeployment.Spec.Template.Spec.Containers[0].Image) ||
		!reflect.DeepEqual(deployment.Spec.Template.Spec.Containers[0].Ports, desiredDeployment.Spec.Template.Spec.Containers[0].Ports) {
		
		deployment.Spec.Replicas = desiredDeployment.Spec.Replicas
		deployment.Spec.Template.Spec.Containers[0].Image = desiredDeployment.Spec.Template.Spec.Containers[0].Image
		deployment.Spec.Template.Spec.Containers[0].Ports = desiredDeployment.Spec.Template.Spec.Containers[0].Ports
		
		return r.Update(ctx, deployment)
	}

	return nil
}

func (r *WebAppReconciler) reconcileService(ctx context.Context, webapp *appsv1alpha1.WebApp) error {
	service := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      webapp.Name,
		Namespace: webapp.Namespace,
	}, service)

	if err != nil && errors.IsNotFound(err) {
		// Service doesn't exist, create it
		service = r.createService(webapp)
		if err := controllerutil.SetControllerReference(webapp, service, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, service)
	} else if err != nil {
		return err
	}

	// Service exists, update if needed
	desiredService := r.createService(webapp)
	if !reflect.DeepEqual(service.Spec.Ports, desiredService.Spec.Ports) {
		service.Spec.Ports = desiredService.Spec.Ports
		return r.Update(ctx, service)
	}

	return nil
}

func (r *WebAppReconciler) createDeployment(webapp *appsv1alpha1.WebApp) *appsv1.Deployment {
	replicas := webapp.Spec.Replicas
	if replicas == 0 {
		replicas = 1
	}

	port := webapp.Spec.Port
	if port == 0 {
		port = 80
	}

	labels := map[string]string{
		"app":        webapp.Name,
		"managed-by": "webapp-operator",
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webapp.Name,
			Namespace: webapp.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "webapp",
							Image: webapp.Spec.Image,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *WebAppReconciler) createService(webapp *appsv1alpha1.WebApp) *corev1.Service {
	port := webapp.Spec.Port
	if port == 0 {
		port = 80
	}

	labels := map[string]string{
		"app":        webapp.Name,
		"managed-by": "webapp-operator",
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webapp.Name,
			Namespace: webapp.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func (r *WebAppReconciler) updateStatus(ctx context.Context, webapp *appsv1alpha1.WebApp) error {
	// Get the Deployment to check available replicas
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      webapp.Name,
		Namespace: webapp.Namespace,
	}, deployment)

	if err != nil {
		return err
	}

	// Update available replicas
	webapp.Status.AvailableReplicas = deployment.Status.AvailableReplicas

	// Update service URL
	webapp.Status.ServiceURL = fmt.Sprintf("%s.%s.svc.cluster.local:%d",
		webapp.Name, webapp.Namespace, webapp.Spec.Port)

	// Update condition
	if deployment.Status.AvailableReplicas == *deployment.Spec.Replicas {
		r.updateCondition(webapp, "Ready", metav1.ConditionTrue, "AllReplicasReady", "All replicas are ready")
	} else {
		r.updateCondition(webapp, "Ready", metav1.ConditionFalse, "ReplicasNotReady",
			fmt.Sprintf("%d/%d replicas ready", deployment.Status.AvailableReplicas, *deployment.Spec.Replicas))
	}

	return r.Status().Update(ctx, webapp)
}

func (r *WebAppReconciler) updateCondition(webapp *appsv1alpha1.WebApp, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	// Find and update existing condition or append new one
	found := false
	for i, c := range webapp.Status.Conditions {
		if c.Type == conditionType {
			webapp.Status.Conditions[i] = condition
			found = true
			break
		}
	}

	if !found {
		webapp.Status.Conditions = append(webapp.Status.Conditions, condition)
	}
}

func (r *WebAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1alpha1.WebApp{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
