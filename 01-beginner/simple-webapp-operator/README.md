# Simple Web App Operator

A beginner-friendly Kubernetes operator that manages web applications by creating and managing Deployments and Services.

## Project Status

**Status**: **Complete and Ready to Use**

- ✅ **CRD Implementation** - WebApp custom resource with validation
- ✅ **Controller Logic** - Full reconciliation for Deployments and Services
- ✅ **Owner References** - Automatic garbage collection
- ✅ **Status Management** - Tracks available replicas and service URL
- ✅ **Conditions** - Reports readiness status with detailed reasons
- ✅ **Build System** - Makefile, Dockerfile, and deployment manifests
- ✅ **RBAC Configuration** - Proper permissions for Deployments and Services
- ✅ **Binary Built** - Ready to run (`bin/manager`)

## Learning Objectives

By completing this project, you will learn:

- Basic reconciliation loop pattern
- Creating and managing Kubernetes resources (Deployments, Services)
- Using owner references for garbage collection
- Updating resource status
- Patching existing resources
- Basic error handling
- Idempotent operations
- Status conditions management

## What This Operator Does

The Simple Web App Operator watches for `WebApp` custom resources and automatically:

1. **Creates a Deployment** with the specified image and replica count
2. **Creates a Service** to expose the application
3. **Updates the WebApp status** with the service URL and available replicas
4. **Handles updates** to the WebApp spec (image, replicas, port)
5. **Cleans up resources** when the WebApp is deleted (via owner references)
6. **Reports conditions** for readiness status and error states

## Prerequisites

- Go 1.26+ (matches project configuration)
- Docker or Podman
- kubectl
- A Kubernetes cluster (kind, minikube, or k3d)
- controller-gen (for CRD generation)

## Quick Start

### 1. Install CRDs

```bash
make install
```

This installs the `WebApp` CRD into your Kubernetes cluster.

### 2. Run the Operator Locally

```bash
make run
```

The operator will start and watch for WebApp resources in your cluster.

### 3. Create a Sample WebApp

In another terminal:

```bash
kubectl apply -f config/samples/apps_v1alpha1_webapp.yaml
```

### 4. Verify It Works

```bash
# Check the WebApp resource
kubectl get webapps

# Check the created Deployment
kubectl get deployments

# Check the created Service
kubectl get services

# Describe the WebApp to see its status
kubectl describe webapp webapp-sample
```

## Project Structure

```
simple-webapp-operator/
├── api/v1alpha1/
│   ├── webapp_types.go          # CRD definition with validation
│   ├── groupversion_info.go     # API group metadata
│   └── zz_generated.deepcopy.go # Generated DeepCopy methods
├── controllers/
│   └── webapp_controller.go     # Full reconciliation logic
├── cmd/
│   └── main.go                  # Controller manager entry point
├── config/
│   ├── crd/bases/               # Generated CRD manifests
│   ├── rbac/                    # RBAC permissions
│   ├── manager/                 # Operator deployment
│   └── samples/                 # Example WebApp resources
├── hack/
│   └── boilerplate.go.txt       # License header for code generation
├── bin/
│   └── manager                  # Built binary
├── Makefile                     # Build and deployment commands
├── Dockerfile                   # Container build configuration
├── PROJECT                      # Kubebuilder metadata
└── go.mod                       # Go module definition
```

## Implementation Details

### The WebApp CRD (`api/v1alpha1/webapp_types.go`)

```go
type WebAppSpec struct {
    // Image is the container image to deploy
    // +kubebuilder:validation:Required
    Image string `json:"image"`
    
    // Replicas is the number of desired pods
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=10
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas,omitempty"`
    
    // Port is the container port to expose
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=65535
    // +kubebuilder:default=80
    Port int32 `json:"port,omitempty"`
}

type WebAppStatus struct {
    // AvailableReplicas is the number of ready pods
    AvailableReplicas int32 `json:"availableReplicas,omitempty"`
    
    // ServiceURL is the URL to access the application
    ServiceURL string `json:"serviceURL,omitempty"`
    
    // Conditions represent the latest available observations
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### The Controller (`controllers/webapp_controller.go`)

The reconciliation loop follows this pattern:

```go
func (r *WebAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // 1. Fetch the WebApp resource
    webapp := &appsv1alpha1.WebApp{}
    if err := r.Get(ctx, req.NamespacedName, webapp); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Reconcile Deployment
    if err := r.reconcileDeployment(ctx, webapp); err != nil {
        return ctrl.Result{}, err
    }
    
    // 3. Reconcile Service
    if err := r.reconcileService(ctx, webapp); err != nil {
        return ctrl.Result{}, err
    }
    
    // 4. Update Status
    if err := r.updateStatus(ctx, webapp); err != nil {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

## Key Concepts Explained

### 1. Owner References

Owner references establish a parent-child relationship between resources:

```go
// Set the WebApp as the owner of the Deployment
if err := controllerutil.SetControllerReference(webapp, deployment, r.Scheme); err != nil {
    return err
}
```

**Benefits:**
- Automatic garbage collection (child deleted when parent is deleted)
- Shows relationships in `kubectl describe`
- Prevents orphaned resources

### 2. Idempotent Reconciliation

The reconcile function should be idempotent - running it multiple times produces the same result:

```go
// Check if Deployment exists
deployment := &appsv1.Deployment{}
err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, deployment)

if err != nil && errors.IsNotFound(err) {
    // Deployment doesn't exist, create it
    return r.Create(ctx, newDeployment)
} else if err != nil {
    return err
}

// Deployment exists, update if needed
if !reflect.DeepEqual(deployment.Spec, desiredSpec) {
    deployment.Spec = desiredSpec
    return r.Update(ctx, deployment)
}
```

### 3. Status Updates

Always update status separately from spec:

```go
// Update status subresource
webapp.Status.AvailableReplicas = deployment.Status.AvailableReplicas
if err := r.Status().Update(ctx, webapp); err != nil {
    return err
}
```

## Testing

### Run Unit Tests

```bash
make test
```

### Run with Coverage

```bash
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Manual Testing

```bash
# Create a WebApp
cat <<EOF | kubectl apply -f -
apiVersion: apps.example.com/v1alpha1
kind: WebApp
metadata:
  name: nginx-app
spec:
  image: nginx:latest
  replicas: 3
  port: 80
EOF

# Check status
kubectl get webapp nginx-app -o yaml

# Update replicas
kubectl patch webapp nginx-app --type='merge' -p '{"spec":{"replicas":5}}'

# Verify deployment updated
kubectl get deployment nginx-app

# Delete WebApp
kubectl delete webapp nginx-app

# Verify cleanup
kubectl get deployments,services -l app=nginx-app
```

## Troubleshooting

### Operator Not Starting

```bash
# Check if CRDs are installed
kubectl get crds | grep webapps

# Reinstall CRDs
make install
```

### Resources Not Being Created

```bash
# Check operator logs
# (if running locally, check terminal output)

# If deployed to cluster:
kubectl logs -n default deployment/simple-webapp-operator-controller
```

### Deployment Not Updating

```bash
# Check WebApp status
kubectl describe webapp <name>

# Check events
kubectl get events --sort-by='.lastTimestamp'
```

## Deployment

### Build and Push Image

```bash
# Build image
make docker-build IMG=<your-registry>/webapp-operator:v0.1.0

# Push image
make docker-push IMG=<your-registry>/webapp-operator:v0.1.0
```

### Deploy to Cluster

```bash
make deploy IMG=<your-registry>/webapp-operator:v0.1.0
```

### Verify Deployment

```bash
kubectl get deployment -n default
kubectl get pods -n default
```

### Undeploy

```bash
make undeploy
```

## Exercises

Try these exercises to deepen your understanding:

### Exercise 1: Add Environment Variables

Extend the WebApp CRD to support environment variables:

```go
type WebAppSpec struct {
    Image    string            `json:"image"` 
    Replicas int32             `json:"replicas,omitempty"` 
    Port     int32             `json:"port,omitempty"` 
    Env      map[string]string `json:"env,omitempty"` // Add this
}
```

Update the controller to pass these to the Deployment.

### Exercise 2: Add Resource Limits

Add CPU and memory limits to the WebApp spec:

```go
type ResourceRequirements struct {
    CPU    string `json:"cpu,omitempty"` 
    Memory string `json:"memory,omitempty"` 
}

type WebAppSpec struct {
    // ... existing fields
    Resources ResourceRequirements `json:"resources,omitempty"` 
}
```

### Exercise 3: Implement Health Checks

Add liveness and readiness probes to the Deployment based on WebApp configuration.

### Exercise 4: Add Finalizers

Implement a finalizer to perform cleanup actions before the WebApp is deleted.

## Next Steps

After completing this project, move on to:

- [ConfigMap Syncer](../configmap-syncer/README.md) - Learn about watching multiple resources
- [Database User Manager](../../02-intermediate/database-user-manager/README.md) - External system integration
- [StatefulSet Backup Operator](../../02-intermediate/statefulset-backup-operator/README.md) - Cron scheduling and job management

## Additional Resources

- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Controller Runtime Documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)

---

**Congratulations on building your first Kubernetes operator!**
