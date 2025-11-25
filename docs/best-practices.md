# Kubernetes Operator Best Practices

A comprehensive guide to building production-ready Kubernetes operators.

## Table of Contents

- [API Design](#api-design)
- [Controller Implementation](#controller-implementation)
- [Error Handling](#error-handling)
- [Status Management](#status-management)
- [Testing](#testing)
- [Security](#security)
- [Performance](#performance)
- [Observability](#observability)

## API Design

### Follow Kubernetes API Conventions

#### Use Proper API Versioning

```go
// Start with v1alpha1 for initial development
// api/v1alpha1/myresource_types.go

// Progress to v1beta1 when API is stable
// api/v1beta1/myresource_types.go

// Release as v1 when production-ready
// api/v1/myresource_types.go
```

#### Spec and Status Pattern

```go
type MyResourceSpec struct {
    // Desired state - what the user wants
    Replicas int32  `json:"replicas"`
    Image    string `json:"image"`
}

type MyResourceStatus struct {
    // Observed state - what actually exists
    AvailableReplicas int32       `json:"availableReplicas,omitempty"`
    Conditions        []Condition `json:"conditions,omitempty"`
}
```

#### Use Conditions for Status

```go
type Condition struct {
    Type               string             `json:"type"`
    Status             corev1.ConditionStatus `json:"status"`
    LastTransitionTime metav1.Time        `json:"lastTransitionTime,omitempty"`
    Reason             string             `json:"reason,omitempty"`
    Message            string             `json:"message,omitempty"`
}

// Common condition types
const (
    ConditionReady      = "Ready"
    ConditionProgressing = "Progressing"
    ConditionDegraded   = "Degraded"
)
```

### Design for Declarative Management

```go
// ✅ Good - Declarative
type WebAppSpec struct {
    Image    string `json:"image"`
    Replicas int32  `json:"replicas"`
}

// ❌ Bad - Imperative
type WebAppSpec struct {
    Action string `json:"action"` // "create", "update", "delete"
}
```

### Use Validation

```go
// +kubebuilder:validation:Minimum=1
// +kubebuilder:validation:Maximum=100
Replicas int32 `json:"replicas"`

// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
Name string `json:"name"`

// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
ServiceType string `json:"serviceType,omitempty"`
```

## Controller Implementation

### Idempotent Reconciliation

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Reconciliation should be idempotent
    // Running it multiple times should produce the same result
    
    // ✅ Good - Check if resource exists, create if not
    deployment := &appsv1.Deployment{}
    err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment)
    if err != nil && errors.IsNotFound(err) {
        // Create deployment
        return r.createDeployment(ctx, webapp)
    }
    
    // Update if needed
    if !reflect.DeepEqual(deployment.Spec, desiredSpec) {
        deployment.Spec = desiredSpec
        return r.Update(ctx, deployment)
    }
    
    return ctrl.Result{}, nil
}
```

### Use Owner References

```go
// Set owner reference for garbage collection
if err := controllerutil.SetControllerReference(webapp, deployment, r.Scheme); err != nil {
    return ctrl.Result{}, err
}

// This ensures:
// 1. Deployment is deleted when WebApp is deleted
// 2. Deployment shows up in kubectl describe webapp
```

### Handle Deletion with Finalizers

```go
const finalizerName = "webapp.example.com/finalizer"

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    webapp := &appsv1alpha1.WebApp{}
    if err := r.Get(ctx, req.NamespacedName, webapp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // Handle deletion
    if !webapp.DeletionTimestamp.IsZero() {
        if controllerutil.ContainsFinalizer(webapp, finalizerName) {
            // Perform cleanup
            if err := r.cleanup(ctx, webapp); err != nil {
                return ctrl.Result{}, err
            }
            
            // Remove finalizer
            controllerutil.RemoveFinalizer(webapp, finalizerName)
            if err := r.Update(ctx, webapp); err != nil {
                return ctrl.Result{}, err
            }
        }
        return ctrl.Result{}, nil
    }
    
    // Add finalizer if not present
    if !controllerutil.ContainsFinalizer(webapp, finalizerName) {
        controllerutil.AddFinalizer(webapp, finalizerName)
        if err := r.Update(ctx, webapp); err != nil {
            return ctrl.Result{}, err
        }
    }
    
    // Normal reconciliation logic
    return r.reconcile(ctx, webapp)
}
```

### Proper Resource Watching

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.WebApp{}).
        Owns(&appsv1.Deployment{}). // Watch owned resources
        Owns(&corev1.Service{}).
        Watches(
            &source.Kind{Type: &corev1.ConfigMap{}},
            handler.EnqueueRequestsFromMapFunc(r.findWebAppsForConfigMap),
        ). // Watch related resources
        Complete(r)
}
```

## Error Handling

### Distinguish Transient from Permanent Errors

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // Transient error - retry
    if err := r.createResource(ctx, resource); err != nil {
        if errors.IsConflict(err) || errors.IsServerTimeout(err) {
            // Requeue with backoff
            return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
        }
        return ctrl.Result{}, err
    }
    
    // Permanent error - don't retry
    if err := validateSpec(resource.Spec); err != nil {
        // Update status with error, don't requeue
        r.updateStatus(ctx, resource, "Invalid", err.Error())
        return ctrl.Result{}, nil
    }
    
    return ctrl.Result{}, nil
}
```

### Use Structured Logging

```go
import "github.com/go-logr/logr"

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // ✅ Good - Structured logging
    log.Info("Reconciling WebApp",
        "namespace", req.Namespace,
        "name", req.Name,
        "replicas", webapp.Spec.Replicas)
    
    // ❌ Bad - Unstructured logging
    log.Info(fmt.Sprintf("Reconciling %s/%s with %d replicas",
        req.Namespace, req.Name, webapp.Spec.Replicas))
}
```

### Implement Retry Logic

```go
import "k8s.io/apimachinery/pkg/util/wait"

func (r *Reconciler) createWithRetry(ctx context.Context, obj client.Object) error {
    return wait.ExponentialBackoff(wait.Backoff{
        Duration: 1 * time.Second,
        Factor:   2.0,
        Steps:    5,
    }, func() (bool, error) {
        err := r.Create(ctx, obj)
        if err == nil {
            return true, nil
        }
        if errors.IsAlreadyExists(err) {
            return true, nil
        }
        // Retry on transient errors
        return false, nil
    })
}
```

## Status Management

### Update Status Subresource

```go
// ✅ Good - Update status separately
webapp.Status.AvailableReplicas = deployment.Status.AvailableReplicas
if err := r.Status().Update(ctx, webapp); err != nil {
    return ctrl.Result{}, err
}

// ❌ Bad - Update entire object
if err := r.Update(ctx, webapp); err != nil {
    return ctrl.Result{}, err
}
```

### Use Status Conditions

```go
func (r *Reconciler) setCondition(webapp *appsv1alpha1.WebApp, conditionType string, status corev1.ConditionStatus, reason, message string) {
    condition := metav1.Condition{
        Type:               conditionType,
        Status:             metav1.ConditionStatus(status),
        LastTransitionTime: metav1.Now(),
        Reason:             reason,
        Message:            message,
    }
    
    meta.SetStatusCondition(&webapp.Status.Conditions, condition)
}

// Usage
r.setCondition(webapp, "Ready", corev1.ConditionTrue, "DeploymentReady", "Deployment is ready")
```

### Provide Meaningful Status Information

```go
type WebAppStatus struct {
    // Replicas
    Replicas          int32 `json:"replicas,omitempty"`
    AvailableReplicas int32 `json:"availableReplicas,omitempty"`
    ReadyReplicas     int32 `json:"readyReplicas,omitempty"`
    
    // Endpoints
    ServiceURL string `json:"serviceURL,omitempty"`
    
    // Conditions
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    
    // Observed generation
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}
```

## Testing

### Unit Tests for Reconciliation Logic

```go
func TestReconcile(t *testing.T) {
    // Setup
    scheme := runtime.NewScheme()
    _ = appsv1alpha1.AddToScheme(scheme)
    _ = appsv1.AddToScheme(scheme)
    
    webapp := &appsv1alpha1.WebApp{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-webapp",
            Namespace: "default",
        },
        Spec: appsv1alpha1.WebAppSpec{
            Image:    "nginx:latest",
            Replicas: 3,
        },
    }
    
    client := fake.NewClientBuilder().
        WithScheme(scheme).
        WithObjects(webapp).
        Build()
    
    reconciler := &Reconciler{
        Client: client,
        Scheme: scheme,
    }
    
    // Test
    req := ctrl.Request{
        NamespacedName: types.NamespacedName{
            Name:      "test-webapp",
            Namespace: "default",
        },
    }
    
    result, err := reconciler.Reconcile(context.Background(), req)
    
    // Assert
    assert.NoError(t, err)
    assert.Equal(t, ctrl.Result{}, result)
    
    // Verify deployment was created
    deployment := &appsv1.Deployment{}
    err = client.Get(context.Background(),
        types.NamespacedName{Name: "test-webapp", Namespace: "default"},
        deployment)
    assert.NoError(t, err)
    assert.Equal(t, int32(3), *deployment.Spec.Replicas)
}
```

### Integration Tests with envtest

```go
import "sigs.k8s.io/controller-runtime/pkg/envtest"

var testEnv *envtest.Environment

func TestMain(m *testing.M) {
    testEnv = &envtest.Environment{
        CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
    }
    
    cfg, err := testEnv.Start()
    if err != nil {
        panic(err)
    }
    
    code := m.Run()
    testEnv.Stop()
    os.Exit(code)
}
```

### E2E Tests

```go
func TestWebAppE2E(t *testing.T) {
    // Create WebApp
    webapp := &appsv1alpha1.WebApp{...}
    err := k8sClient.Create(ctx, webapp)
    require.NoError(t, err)
    
    // Wait for deployment to be ready
    Eventually(func() bool {
        deployment := &appsv1.Deployment{}
        err := k8sClient.Get(ctx, types.NamespacedName{
            Name: webapp.Name, Namespace: webapp.Namespace,
        }, deployment)
        return err == nil && deployment.Status.ReadyReplicas == 3
    }, timeout, interval).Should(BeTrue())
    
    // Verify service exists
    service := &corev1.Service{}
    err = k8sClient.Get(ctx, types.NamespacedName{
        Name: webapp.Name, Namespace: webapp.Namespace,
    }, service)
    require.NoError(t, err)
}
```

## Security

### RBAC Permissions

```go
// +kubebuilder:rbac:groups=apps.example.com,resources=webapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.example.com,resources=webapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

// Follow principle of least privilege
// Only request permissions you actually need
```

### Validate User Input

```go
// Use webhook validation
// +kubebuilder:webhook:path=/validate-apps-example-com-v1alpha1-webapp,mutating=false,failurePolicy=fail,groups=apps.example.com,resources=webapps,verbs=create;update,versions=v1alpha1,name=vwebapp.kb.io

func (r *WebApp) ValidateCreate() error {
    if r.Spec.Replicas < 1 || r.Spec.Replicas > 100 {
        return fmt.Errorf("replicas must be between 1 and 100")
    }
    return nil
}
```

### Secure Secret Handling

```go
// Don't log secrets
log.Info("Creating database user", "username", user.Spec.Username)
// ❌ Don't do this: log.Info("Password", "password", password)

// Use secret references, not inline secrets
type DatabaseSpec struct {
    PasswordSecretRef corev1.SecretReference `json:"passwordSecretRef"`
    // ❌ Not: Password string `json:"password"`
}
```

## Performance

### Use Caching Effectively

```go
// controller-runtime caches by default
// Avoid unnecessary API calls

// ✅ Good - Uses cache
deployment := &appsv1.Deployment{}
err := r.Get(ctx, namespacedName, deployment)

// ❌ Bad - Direct API call
deployment, err := r.ClientSet.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
```

### Implement Rate Limiting

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.WebApp{}).
        WithOptions(controller.Options{
            MaxConcurrentReconciles: 3,
            RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(
                1*time.Second,
                60*time.Second,
            ),
        }).
        Complete(r)
}
```

### Use Predicates to Filter Events

```go
import "sigs.k8s.io/controller-runtime/pkg/predicate"

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.WebApp{}).
        WithEventFilter(predicate.Funcs{
            UpdateFunc: func(e event.UpdateEvent) bool {
                // Only reconcile if spec changed
                oldWebApp := e.ObjectOld.(*appsv1alpha1.WebApp)
                newWebApp := e.ObjectNew.(*appsv1alpha1.WebApp)
                return !reflect.DeepEqual(oldWebApp.Spec, newWebApp.Spec)
            },
        }).
        Complete(r)
}
```

## Observability

### Metrics

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
    reconcileTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "webapp_reconcile_total",
            Help: "Total number of reconciliations",
        },
        []string{"namespace", "name", "result"},
    )
)

func init() {
    metrics.Registry.MustRegister(reconcileTotal)
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    result, err := r.reconcile(ctx, req)
    
    status := "success"
    if err != nil {
        status = "error"
    }
    
    reconcileTotal.WithLabelValues(req.Namespace, req.Name, status).Inc()
    
    return result, err
}
```

### Health Checks

```go
import "sigs.k8s.io/controller-runtime/pkg/healthz"

func main() {
    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        HealthProbeBindAddress: ":8081",
    })
    
    if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
        setupLog.Error(err, "unable to set up health check")
        os.Exit(1)
    }
    
    if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
        setupLog.Error(err, "unable to set up ready check")
        os.Exit(1)
    }
}
```

### Tracing

```go
import "go.opentelemetry.io/otel"

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    ctx, span := otel.Tracer("webapp-controller").Start(ctx, "Reconcile")
    defer span.End()
    
    span.SetAttributes(
        attribute.String("namespace", req.Namespace),
        attribute.String("name", req.Name),
    )
    
    return r.reconcile(ctx, req)
}
```

## Additional Best Practices

### Documentation

- Document your CRDs with comments
- Provide examples in `config/samples/`
- Write comprehensive README files
- Include troubleshooting guides

### Versioning

- Use semantic versioning for releases
- Maintain backwards compatibility
- Provide migration guides for breaking changes

### CI/CD

- Run tests on every PR
- Build and push images automatically
- Use linters (golangci-lint)
- Generate and validate CRDs

---

**Follow these best practices to build robust, production-ready operators!** 🚀
