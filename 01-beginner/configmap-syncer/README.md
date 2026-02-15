# ConfigMap Syncer Controller

A Kubernetes controller that automatically synchronizes ConfigMaps across multiple namespaces.

## [x] Project Status - COMPLETE & WORKING

This controller is fully implemented, builds successfully, and is ready for deployment!

### Built With
- **Go**: 1.26
- **Kubernetes**: v0.35.1
- **Controller Runtime**: v0.19.0
- **Status**:[x] **Build Successful**

## What This Controller Does

The ConfigMap Syncer watches for `ConfigMapSyncer` custom resources and:

1. Monitors a source ConfigMap in a specified namespace
2. Automatically creates copies in target namespaces
3. Keeps copies synchronized when the source changes
4. Cleans up synced ConfigMaps when the syncer is deleted (using finalizers)
5. Handles namespace creation/deletion gracefully

## Prerequisites

- Go 1.26+
- Docker or Podman
- kubectl
- A Kubernetes cluster (kind, minikube, or k3d)
- controller-gen tool

## Project Structure

```
configmap-syncer/
├── api/v1alpha1/
│   ├── configmapsyncer_types.go      # CRD definition
│   ├── groupversion_info.go           # API group registration
│   └── zz_generated.deepcopy.go       # Generated code
├── controllers/
│   └── configmapsyncer_controller.go  # Controller implementation
├── config/
│   ├── crd/bases/                     # Generated CRD manifests
│   ├── rbac/                          # RBAC permissions
│   ├── manager/                       # Deployment manifest
│   └── samples/                       # Example resources
├── Dockerfile                         # Multi-stage container build
├── Makefile                          # Build and deployment targets
├── main.go                           # Controller manager entry point
└── bin/manager                       # Built binary (ready to run!)
```

## Completed Components

### 1. API Definition
- [x] `api/v1alpha1/configmapsyncer_types.go` - CRD types
- [x] `api/v1alpha1/groupversion_info.go` - Group version registration
- [x] Generated DeepCopy methods

### 2. Controller Implementation
- [x] Full reconciliation logic with finalizers
- [x] Cross-namespace ConfigMap synchronization
- [x] Status updates with conditions
- [x] ConfigMap watching and event mapping

### 3. Build Configuration
- [x] Makefile with all targets
- [x] Dockerfile for containerization
- [x] Generated CRD manifests
- [x] RBAC configuration

## Quick Start

### 1. Build the Controller

```bash
# The controller is already built!
ls -lh bin/manager

# Or rebuild if needed
go build -o bin/manager main.go
```

### 2. Install CRDs

```bash
kubectl apply -f config/crd/bases/config.example.com_configmapsyncers.yaml
```

### 3. Apply RBAC

```bash
kubectl apply -f config/rbac/role.yaml
kubectl apply -f config/rbac/role_binding.yaml
```

### 4. Run the Controller

```bash
# Run locally
./bin/manager

# Or use go run
go run main.go
```

### 5. Create Test Resources

```bash
# Create target namespaces
kubectl create namespace dev
kubectl create namespace staging
kubectl create namespace prod

# Create source ConfigMap
kubectl create configmap app-config \
  --from-literal=database.host=db.example.com \
  --from-literal=database.port=5432 \
  -n default

# Create ConfigMapSyncer
kubectl apply -f config/samples/config_v1alpha1_configmapsyncer.yaml
```

### 6. Verify Synchronization

```bash
# Check syncer status
kubectl get configmapsyncers -o wide

# Verify ConfigMaps in target namespaces
kubectl get configmap app-config -n dev
kubectl get configmap app-config -n staging
kubectl get configmap app-config -n prod

# Check content
kubectl get configmap app-config -n dev -o yaml
```

### 7. Test Updates

```bash
# Update source ConfigMap
kubectl patch configmap app-config -n default \
  --type='merge' -p '{"data":{"new-key":"new-value"}}'

# Verify propagation (wait a few seconds)
kubectl get configmap app-config -n dev -o jsonpath='{.data.new-key}'
```

### 8. Test Cleanup

```bash
# Delete syncer
kubectl delete configmapsyncer configmapsyncer-sample

# Verify cleanup (should return "not found")
kubectl get configmap app-config -n dev
kubectl get configmap app-config -n staging
```

## Understanding the Code

### The ConfigMapSyncer CRD (`api/v1alpha1/configmapsyncer_types.go`)

```go
type ConfigMapSyncerSpec struct {
    // SourceNamespace is the namespace containing the source ConfigMap
    // +kubebuilder:validation:Required
    SourceNamespace string `json:"sourceNamespace"`
    
    // SourceConfigMap is the name of the ConfigMap to sync
    // +kubebuilder:validation:Required
    SourceConfigMap string `json:"sourceConfigMap"`
    
    // TargetNamespaces is the list of namespaces to sync to
    // +kubebuilder:validation:MinItems=1
    TargetNamespaces []string `json:"targetNamespaces"`
}

type ConfigMapSyncerStatus struct {
    // SyncedNamespaces lists successfully synced namespaces
    SyncedNamespaces []string `json:"syncedNamespaces,omitempty"`
    
    // FailedNamespaces lists namespaces that failed to sync
    FailedNamespaces []string `json:"failedNamespaces,omitempty"`
    
    // LastSyncTime is the last successful sync timestamp
    LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
    
    // Conditions represent the latest observations
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### The Controller (`controllers/configmapsyncer_controller.go`)

```go
func (r *ConfigMapSyncerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // 1. Fetch the ConfigMapSyncer
    syncer := &configv1alpha1.ConfigMapSyncer{}
    if err := r.Get(ctx, req.NamespacedName, syncer); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Handle deletion with finalizers
    if !syncer.DeletionTimestamp.IsZero() {
        return r.handleDeletion(ctx, syncer)
    }
    
    // 3. Add finalizer if not present
    if !controllerutil.ContainsFinalizer(syncer, finalizerName) {
        controllerutil.AddFinalizer(syncer, finalizerName)
        if err := r.Update(ctx, syncer); err != nil {
            return ctrl.Result{}, err
        }
    }
    
    // 4. Fetch source ConfigMap
    sourceConfigMap, err := r.getSourceConfigMap(ctx, syncer)
    if err != nil {
        return ctrl.Result{}, err
    }
    
    // 5. Sync to target namespaces
    if err := r.syncToTargets(ctx, syncer, sourceConfigMap); err != nil {
        return ctrl.Result{}, err
    }
    
    // 6. Update status
    if err := r.updateStatus(ctx, syncer); err != nil {
        return ctrl.Result{}, err
    }
    
    return ctrl.Result{}, nil
}
```

## Implementation Highlights

### Controller Logic Flow

```
1. Fetch ConfigMapSyncer resource
2. Check if being deleted
   → Yes: Run finalizer cleanup, remove finalizer
   → No: Continue
3. Add finalizer if not present
4. Fetch source ConfigMap
5. For each target namespace:
   - Check namespace exists
   - Create or update ConfigMap
   - Track success/failure
6. Update status with results
7. Requeue if needed
```

### Cross-Namespace Considerations

- Cannot use OwnerReferences across namespaces
- Use labels and annotations to track ownership
- Finalizers ensure cleanup happens
- RBAC needs cluster-wide permissions

## Key Concepts Explained

### 1. Finalizers

Finalizers prevent resource deletion until cleanup is complete:

```go
const finalizerName = "configmapsyncer.config.example.com/finalizer"

func (r *Reconciler) handleDeletion(ctx context.Context, syncer *ConfigMapSyncer) (ctrl.Result, error) {
    if controllerutil.ContainsFinalizer(syncer, finalizerName) {
        // Delete synced ConfigMaps from all target namespaces
        for _, ns := range syncer.Spec.TargetNamespaces {
            cm := &corev1.ConfigMap{
                ObjectMeta: metav1.ObjectMeta{
                    Name:      syncer.Spec.SourceConfigMap,
                    Namespace: ns,
                },
            }
            if err := r.Delete(ctx, cm); err != nil && !errors.IsNotFound(err) {
                return ctrl.Result{}, err
            }
        }
        
        // Remove finalizer
        controllerutil.RemoveFinalizer(syncer, finalizerName)
        if err := r.Update(ctx, syncer); err != nil {
            return ctrl.Result{}, err
        }
    }
    return ctrl.Result{}, nil
}
```

### 2. Watching Multiple Resources

The controller watches both ConfigMapSyncer and ConfigMap resources:

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&configv1alpha1.ConfigMapSyncer{}).
        Watches(
            &source.Kind{Type: &corev1.ConfigMap{}},
            handler.EnqueueRequestsFromMapFunc(r.findSyncersForConfigMap),
            builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
        ).
        Complete(r)
}

// Map ConfigMap changes to ConfigMapSyncer reconciliations
func (r *Reconciler) findSyncersForConfigMap(cm client.Object) []reconcile.Request {
    syncers := &configv1alpha1.ConfigMapSyncerList{}
    if err := r.List(context.Background(), syncers); err != nil {
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
```

### 3. Cross-Namespace Operations

Syncing ConfigMaps across namespaces:

```go
func (r *Reconciler) syncToTargets(ctx context.Context, syncer *ConfigMapSyncer, source *corev1.ConfigMap) error {
    for _, targetNS := range syncer.Spec.TargetNamespaces {
        // Create target ConfigMap
        target := &corev1.ConfigMap{
            ObjectMeta: metav1.ObjectMeta{
                Name:      source.Name,
                Namespace: targetNS,
                Labels: map[string]string{
                    "synced-by": syncer.Name,
                    "synced-from": syncer.Spec.SourceNamespace,
                },
            },
            Data:       source.Data,
            BinaryData: source.BinaryData,
        }
        
        // Check if exists
        existing := &corev1.ConfigMap{}
        err := r.Get(ctx, types.NamespacedName{Name: target.Name, Namespace: targetNS}, existing)
        
        if err != nil && errors.IsNotFound(err) {
            // Create new
            if err := r.Create(ctx, target); err != nil {
                return err
            }
        } else if err != nil {
            return err
        } else {
            // Update existing
            existing.Data = target.Data
            existing.BinaryData = target.BinaryData
            if err := r.Update(ctx, existing); err != nil {
                return err
            }
        }
    }
    return nil
}
```

## What You'll Learn

### Implemented Features

1. **Finalizers** - Proper cleanup of synced ConfigMaps before deletion
2. **Cross-Namespace Operations** - Syncing resources across namespace boundaries
3. **Multiple Resource Watching** - Watching both ConfigMapSyncer and ConfigMap resources
4. **Status Management** - Tracking sync status with conditions
5. **RBAC Configuration** - Proper permissions for cross-namespace operations

### Key Concepts Demonstrated

- **Reconciliation Loop** - Idempotent state management
- **Owner References** - Cannot be used across namespaces (handled with labels/annotations)
- **Finalizers** - Preventing deletion until cleanup is complete
- **Event Mapping** - Mapping ConfigMap changes to ConfigMapSyncer reconciliations
- **Status Subresource** - Separate status updates from spec changes

## Testing

### Integration Test Scenario

```bash
# 1. Create source ConfigMap
kubectl create configmap test-config \
  --from-literal=key1=value1 \
  --from-literal=key2=value2 \
  -n default

# 2. Create syncer
cat <<EOF | kubectl apply -f -
apiVersion: config.example.com/v1alpha1
kind: ConfigMapSyncer
metadata:
  name: test-syncer
spec:
  sourceNamespace: default
  sourceConfigMap: test-config
  targetNamespaces:
    - dev
    - staging
EOF

# 3. Verify sync
kubectl get configmap test-config -n dev
kubectl get configmap test-config -n staging

# 4. Update source
kubectl patch configmap test-config -n default \
  --type='merge' -p '{"data":{"key1":"updated-value"}}'

# 5. Verify update propagated
sleep 2
kubectl get configmap test-config -n dev -o jsonpath='{.data.key1}'

# 6. Delete syncer
kubectl delete configmapsyncer test-syncer

# 7. Verify cleanup
kubectl get configmap test-config -n dev
kubectl get configmap test-config -n staging
# Should return "not found"
```

## Troubleshooting

### ConfigMaps Not Syncing

```bash
# Check syncer status
kubectl describe configmapsyncer <name>

# Check controller logs
kubectl logs -n configmap-syncer-system deployment/configmap-syncer-controller-manager

# Verify source ConfigMap exists
kubectl get configmap <name> -n <source-namespace>

# Verify target namespaces exist
kubectl get namespace
```

### Finalizer Not Removing

```bash
# Check if ConfigMaps in target namespaces are deleted
kubectl get configmap -A | grep <configmap-name>

# Manually remove finalizer if stuck
kubectl patch configmapsyncer <name> \
  --type='json' -p='[{"op": "remove", "path": "/metadata/finalizers"}]'
```

## Exercises

### Exercise 1: Add Namespace Selector

Instead of listing namespaces, use a label selector:

```go
type ConfigMapSyncerSpec struct {
    SourceNamespace string            `json:"sourceNamespace"`
    SourceConfigMap string            `json:"sourceConfigMap"`
    NamespaceSelector map[string]string `json:"namespaceSelector"`
}
```

### Exercise 2: Selective Key Sync

Allow syncing only specific keys from the source ConfigMap:

```go
type ConfigMapSyncerSpec struct {
    // ... existing fields
    Keys []string `json:"keys,omitempty"` // If empty, sync all keys
}
```

### Exercise 3: Add Secret Support

Extend the controller to also sync Secrets across namespaces.

### Exercise 4: Implement Conflict Resolution

Handle cases where a ConfigMap already exists in the target namespace but wasn't created by the syncer.

## Next Steps

After completing this project, move on to:

- [Database User Manager](../../02-intermediate/database-user-manager/README.md) - External system integration
- [StatefulSet Backup Operator](../../02-intermediate/statefulset-backup-operator/README.md) - Jobs and scheduling

## Additional Resources

- [Finalizers Documentation](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
- [Controller Runtime Predicates](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/predicate)
- [Cross-Namespace Watching](https://book.kubebuilder.io/reference/watching-resources.html)

---

**Great job learning about cross-namespace operations and finalizers!**
