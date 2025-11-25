# Application Rolling Upgrade Operator

Orchestrate complex application upgrades with health checks, database migrations, and rollback capabilities.

## 📚 Learning Objectives

- ✅ Day 2 operations modeling
- ✅ Complex state management
- ✅ Health checking
- ✅ Database migrations
- ✅ Rollback logic
- ✅ Upgrade orchestration

## 🎯 What This Operator Does

Watches for `ManagedApplication` resources and:

1. Orchestrates multi-step application upgrades
2. Runs database migrations before deployment
3. Performs health checks at each stage
4. Supports canary deployments
5. Automatic rollback on failure
6. Manages upgrade windows and scheduling

## 📋 Prerequisites

- Go 1.21+, Docker, kubectl, Kubernetes cluster
- Kubebuilder v3.x
- Understanding of deployment strategies

## 🚀 Quick Start

### 1. Create a ManagedApplication

```bash
kubectl apply -f - <<EOF
apiVersion: apps.example.com/v1alpha1
kind: ManagedApplication
metadata:
  name: my-app
spec:
  version: v2.0.0
  paused: false
  upgradeStrategy:
    type: RollingWithMigration
    migrationJob:
      image: my-app-migrations:v2.0.0
      command: ["/bin/migrate", "up"]
    healthCheck:
      httpGet:
        path: /health
        port: 8080
      initialDelaySeconds: 30
      periodSeconds: 10
    canary:
      enabled: true
      steps:
        - weight: 10
          pause: 300
        - weight: 50
          pause: 300
        - weight: 100
EOF
```

### 2. Monitor Upgrade Progress

```bash
# Watch upgrade status
kubectl get managedapplication my-app -w

# Check migration job
kubectl get jobs -l app=my-app,type=migration

# View upgrade events
kubectl describe managedapplication my-app
```

## 📖 Key Code Snippets

### CRD Definition

```go
type ManagedApplicationSpec struct {
    Version         string          `json:"version"`
    Paused          bool            `json:"paused"`
    UpgradeStrategy UpgradeStrategy `json:"upgradeStrategy"`
}

type UpgradeStrategy struct {
    Type         string       `json:"type"` // Rolling, Canary, BlueGreen
    MigrationJob *JobSpec     `json:"migrationJob,omitempty"`
    HealthCheck  *HealthCheck `json:"healthCheck,omitempty"`
    Canary       *CanarySpec  `json:"canary,omitempty"`
}

type ManagedApplicationStatus struct {
    Phase          string             `json:"phase"` // Pending, Migrating, Deploying, Healthy, Failed
    CurrentVersion string             `json:"currentVersion"`
    TargetVersion  string             `json:"targetVersion"`
    CanaryWeight   int32              `json:"canaryWeight,omitempty"`
    Conditions     []metav1.Condition `json:"conditions,omitempty"`
}
```

### Upgrade Orchestration

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    app := &ManagedApplication{}
    if err := r.Get(ctx, req.NamespacedName, app); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // Check if paused
    if app.Spec.Paused {
        return ctrl.Result{}, nil
    }
    
    // Check if upgrade needed
    if app.Status.CurrentVersion == app.Spec.Version {
        return ctrl.Result{}, nil
    }
    
    // Execute upgrade phases
    switch app.Status.Phase {
    case "":
        return r.startUpgrade(ctx, app)
    case "Migrating":
        return r.runMigration(ctx, app)
    case "Deploying":
        return r.deployNewVersion(ctx, app)
    case "HealthChecking":
        return r.performHealthCheck(ctx, app)
    case "Failed":
        return r.rollback(ctx, app)
    }
    
    return ctrl.Result{}, nil
}
```

### Migration Execution

```go
func (r *Reconciler) runMigration(ctx context.Context, app *ManagedApplication) (ctrl.Result, error) {
    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s-migration-%s", app.Name, app.Spec.Version),
            Namespace: app.Namespace,
        },
        Spec: batchv1.JobSpec{
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyNever,
                    Containers: []corev1.Container{
                        {
                            Name:    "migration",
                            Image:   app.Spec.UpgradeStrategy.MigrationJob.Image,
                            Command: app.Spec.UpgradeStrategy.MigrationJob.Command,
                        },
                    },
                },
            },
        },
    }
    
    // Check if job exists
    existing := &batchv1.Job{}
    err := r.Get(ctx, types.NamespacedName{Name: job.Name, Namespace: job.Namespace}, existing)
    
    if err != nil && errors.IsNotFound(err) {
        // Create migration job
        if err := r.Create(ctx, job); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
    }
    
    // Check job status
    if existing.Status.Succeeded > 0 {
        app.Status.Phase = "Deploying"
        return r.Status().Update(ctx, app), nil
    }
    
    if existing.Status.Failed > 0 {
        app.Status.Phase = "Failed"
        return r.Status().Update(ctx, app), nil
    }
    
    return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}
```

### Canary Deployment

```go
func (r *Reconciler) deployCanary(ctx context.Context, app *ManagedApplication) (ctrl.Result, error) {
    canary := app.Spec.UpgradeStrategy.Canary
    currentStep := r.getCurrentCanaryStep(app)
    
    if currentStep >= len(canary.Steps) {
        // Canary complete, promote to 100%
        return r.promoteCanary(ctx, app)
    }
    
    step := canary.Steps[currentStep]
    
    // Update traffic weight
    if err := r.updateTrafficWeight(ctx, app, step.Weight); err != nil {
        return ctrl.Result{}, err
    }
    
    app.Status.CanaryWeight = step.Weight
    if err := r.Status().Update(ctx, app); err != nil {
        return ctrl.Result{}, err
    }
    
    // Wait for pause duration
    return ctrl.Result{RequeueAfter: time.Duration(step.Pause) * time.Second}, nil
}
```

## 🎓 Exercises

1. **Blue-Green Deployments** - Implement blue-green strategy
2. **Automated Rollback** - Auto-rollback on metric thresholds
3. **Multi-Service Upgrades** - Coordinate upgrades across services
4. **Upgrade Windows** - Schedule upgrades during maintenance windows

## 📚 Additional Resources

- [Progressive Delivery](https://www.weave.works/blog/what-is-progressive-delivery-all-about)
- [Flagger](https://flagger.app/) - Progressive delivery operator
- [Argo Rollouts](https://argoproj.github.io/argo-rollouts/)

---

**Congratulations on mastering complex upgrade orchestration!** 🎉🚀
