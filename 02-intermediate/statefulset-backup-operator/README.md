# StatefulSet Backup Operator

A Kubernetes operator that schedules and executes backups for StatefulSet persistent volumes.

## 📚 Learning Objectives

By completing this project, you will learn:

- ✅ Creating and managing Kubernetes Jobs
- ✅ Implementing cron-based scheduling
- ✅ Working with PersistentVolumeClaims (PVCs)
- ✅ Managing backup lifecycle
- ✅ Status reporting and conditions
- ✅ Handling long-running operations

## 🎯 What This Operator Does

The StatefulSet Backup Operator watches for `BackupPolicy` custom resources and:

1. Schedules periodic backups based on cron expressions
2. Creates Kubernetes Jobs to perform backups
3. Manages backup retention policies
4. Reports backup status and history
5. Supports multiple backup strategies (snapshot, tar, custom)
6. Handles backup failures and retries

## 📋 Prerequisites

- Go 1.21+
- Docker or Podman
- kubectl
- A Kubernetes cluster with storage provisioner
- Kubebuilder v3.x
- Completion of beginner projects recommended

## 🚀 Quick Start

### 1. Set Up a StatefulSet with PVCs

```bash
# Create a PostgreSQL StatefulSet for testing
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  clusterIP: None
  selector:
    app: postgres
  ports:
  - port: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:15
        env:
        - name: POSTGRES_PASSWORD
          value: mysecretpassword
        volumeMounts:
        - name: data
          mountPath: /var/lib/postgresql/data
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
EOF
```

### 2. Install the Operator

```bash
make install
make run
```

### 3. Create a BackupPolicy

```bash
kubectl apply -f config/samples/backup_v1alpha1_backuppolicy.yaml
```

### 4. Verify Backups

```bash
# Check backup policy status
kubectl get backuppolicy

# List backup jobs
kubectl get jobs -l backup-policy=db-backup

# Check backup history
kubectl describe backuppolicy db-backup
```

## 📖 Understanding the Code

### The BackupPolicy CRD (`api/v1alpha1/backuppolicy_types.go`)

```go
type BackupPolicySpec struct {
    // Schedule in cron format
    // +kubebuilder:validation:Required
    Schedule string `json:"schedule"`
    
    // PVCSelector selects PVCs to backup
    // +kubebuilder:validation:Required
    PVCSelector metav1.LabelSelector `json:"pvcSelector"`
    
    // BackupStrategy defines how to perform backups
    // +kubebuilder:validation:Enum=snapshot;tar;custom
    // +kubebuilder:default=tar
    BackupStrategy string `json:"backupStrategy,omitempty"`
    
    // RetentionPolicy defines how many backups to keep
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:default=7
    RetentionCount int32 `json:"retentionCount,omitempty"`
    
    // BackupImage is the container image for backup jobs
    // +kubebuilder:default="busybox:latest"
    BackupImage string `json:"backupImage,omitempty"`
    
    // Suspend pauses backup scheduling
    Suspend bool `json:"suspend,omitempty"`
}

type BackupPolicyStatus struct {
    // LastScheduleTime is when the last backup was scheduled
    LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
    
    // LastSuccessfulTime is when the last backup succeeded
    LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`
    
    // BackupHistory contains recent backup information
    BackupHistory []BackupRecord `json:"backupHistory,omitempty"`
    
    // Conditions represent the latest observations
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type BackupRecord struct {
    // JobName is the name of the backup job
    JobName string `json:"jobName"`
    
    // StartTime is when the backup started
    StartTime metav1.Time `json:"startTime"`
    
    // CompletionTime is when the backup completed
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
    
    // Status is the backup status (Pending, Running, Succeeded, Failed)
    Status string `json:"status"`
    
    // Message provides additional information
    Message string `json:"message,omitempty"`
}
```

### The Controller (`controllers/backuppolicy_controller.go`)

```go
func (r *BackupPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    log := log.FromContext(ctx)
    
    // 1. Fetch the BackupPolicy
    policy := &backupv1alpha1.BackupPolicy{}
    if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Check if suspended
    if policy.Spec.Suspend {
        log.Info("Backup policy is suspended")
        return ctrl.Result{}, nil
    }
    
    // 3. Check if it's time for a backup
    nextSchedule, err := r.getNextScheduleTime(policy)
    if err != nil {
        return ctrl.Result{}, err
    }
    
    now := time.Now()
    if now.Before(nextSchedule) {
        // Requeue at next schedule time
        return ctrl.Result{RequeueAfter: nextSchedule.Sub(now)}, nil
    }
    
    // 4. Find PVCs to backup
    pvcs, err := r.findPVCsToBackup(ctx, policy)
    if err != nil {
        return ctrl.Result{}, err
    }
    
    // 5. Create backup jobs
    for _, pvc := range pvcs {
        if err := r.createBackupJob(ctx, policy, pvc); err != nil {
            return ctrl.Result{}, err
        }
    }
    
    // 6. Clean up old backups
    if err := r.cleanupOldBackups(ctx, policy); err != nil {
        return ctrl.Result{}, err
    }
    
    // 7. Update status
    policy.Status.LastScheduleTime = &metav1.Time{Time: now}
    if err := r.Status().Update(ctx, policy); err != nil {
        return ctrl.Result{}, err
    }
    
    // 8. Requeue for next schedule
    nextSchedule, _ = r.getNextScheduleTime(policy)
    return ctrl.Result{RequeueAfter: nextSchedule.Sub(time.Now())}, nil
}
```

## 🔍 Key Concepts Explained

### 1. Cron Scheduling

```go
import "github.com/robfig/cron/v3"

func (r *Reconciler) getNextScheduleTime(policy *BackupPolicy) (time.Time, error) {
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
```

### 2. Creating Backup Jobs

```go
func (r *Reconciler) createBackupJob(ctx context.Context, policy *BackupPolicy, pvc *corev1.PersistentVolumeClaim) error {
    jobName := fmt.Sprintf("backup-%s-%s", pvc.Name, time.Now().Format("20060102-150405"))
    
    job := &batchv1.Job{
        ObjectMeta: metav1.ObjectMeta{
            Name:      jobName,
            Namespace: policy.Namespace,
            Labels: map[string]string{
                "backup-policy": policy.Name,
                "pvc":           pvc.Name,
            },
        },
        Spec: batchv1.JobSpec{
            Template: corev1.PodTemplateSpec{
                Spec: corev1.PodSpec{
                    RestartPolicy: corev1.RestartPolicyNever,
                    Containers: []corev1.Container{
                        {
                            Name:  "backup",
                            Image: policy.Spec.BackupImage,
                            Command: []string{
                                "/bin/sh",
                                "-c",
                                r.getBackupCommand(policy, pvc),
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
                                    ClaimName: "backup-storage",
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

func (r *Reconciler) getBackupCommand(policy *BackupPolicy, pvc *corev1.PersistentVolumeClaim) string {
    timestamp := time.Now().Format("20060102-150405")
    backupFile := fmt.Sprintf("/backup/%s-%s.tar.gz", pvc.Name, timestamp)
    
    switch policy.Spec.BackupStrategy {
    case "tar":
        return fmt.Sprintf("tar czf %s -C /data .", backupFile)
    case "snapshot":
        // Implementation depends on storage provider
        return "echo 'Snapshot not implemented'"
    default:
        return fmt.Sprintf("tar czf %s -C /data .", backupFile)
    }
}
```

### 3. Monitoring Job Status

```go
func (r *Reconciler) updateBackupHistory(ctx context.Context, policy *BackupPolicy) error {
    // List jobs for this policy
    jobList := &batchv1.JobList{}
    if err := r.List(ctx, jobList, client.InNamespace(policy.Namespace),
        client.MatchingLabels{"backup-policy": policy.Name}); err != nil {
        return err
    }
    
    var history []BackupRecord
    for _, job := range jobList.Items {
        record := BackupRecord{
            JobName:   job.Name,
            StartTime: *job.Status.StartTime,
        }
        
        if job.Status.Succeeded > 0 {
            record.Status = "Succeeded"
            record.CompletionTime = job.Status.CompletionTime
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
    
    // Keep only recent history
    if len(history) > 10 {
        history = history[:10]
    }
    
    policy.Status.BackupHistory = history
    return nil
}
```

### 4. Retention Policy

```go
func (r *Reconciler) cleanupOldBackups(ctx context.Context, policy *BackupPolicy) error {
    jobList := &batchv1.JobList{}
    if err := r.List(ctx, jobList, client.InNamespace(policy.Namespace),
        client.MatchingLabels{"backup-policy": policy.Name}); err != nil {
        return err
    }
    
    // Sort jobs by creation time
    sort.Slice(jobList.Items, func(i, j int) bool {
        return jobList.Items[i].CreationTimestamp.After(jobList.Items[j].CreationTimestamp.Time)
    })
    
    // Delete jobs beyond retention count
    for i := int(policy.Spec.RetentionCount); i < len(jobList.Items); i++ {
        job := &jobList.Items[i]
        if err := r.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
            return err
        }
    }
    
    return nil
}
```

## 🧪 Testing

### Manual Testing

```bash
# 1. Create a backup policy with frequent schedule (every minute for testing)
cat <<EOF | kubectl apply -f -
apiVersion: backup.example.com/v1alpha1
kind: BackupPolicy
metadata:
  name: test-backup
spec:
  schedule: "*/1 * * * *"  # Every minute
  pvcSelector:
    matchLabels:
      app: postgres
  backupStrategy: tar
  retentionCount: 3
EOF

# 2. Wait and check for backup jobs
sleep 70
kubectl get jobs -l backup-policy=test-backup

# 3. Check backup policy status
kubectl describe backuppolicy test-backup

# 4. Verify backup files (if using shared storage)
kubectl exec -it postgres-0 -- ls -lh /backup/

# 5. Test suspension
kubectl patch backuppolicy test-backup --type='merge' -p '{"spec":{"suspend":true}}'

# 6. Verify no new backups are created
sleep 70
kubectl get jobs -l backup-policy=test-backup

# 7. Test retention
kubectl patch backuppolicy test-backup --type='merge' -p '{"spec":{"suspend":false}}'
sleep 300  # Wait for multiple backups
kubectl get jobs -l backup-policy=test-backup
# Should see only 3 most recent jobs
```

## 🎓 Exercises

### Exercise 1: Add Backup Verification

Implement a verification step that checks backup integrity after creation.

### Exercise 2: Support Multiple Backup Destinations

Allow backing up to multiple destinations (S3, GCS, Azure Blob).

### Exercise 3: Implement Restore Functionality

Create a `BackupRestore` CRD that can restore from a backup.

### Exercise 4: Add Notifications

Send notifications (Slack, email) when backups succeed or fail.

### Exercise 5: Incremental Backups

Implement incremental backup support to save storage space.

## 🔗 Next Steps

- [Database User Manager](../database-user-manager/README.md) - External system integration
- [HPA Custom Metric Operator](../hpa-custom-metric-operator/README.md) - Advanced scaling

## 📚 Additional Resources

- [Kubernetes Jobs Documentation](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
- [Cron Library](https://github.com/robfig/cron)
- [PersistentVolumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)

---

**Excellent work on learning about Jobs and scheduling!** 🎉
