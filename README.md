# Kubernetes Operators & Controllers Learning Projects

A collection of hands-on Kubernetes operator and controller projects organized by complexity level. Each project demonstrates key concepts in building production-grade Kubernetes controllers using Kubebuilder or Operator SDK.

## Repository Structure

```
k8s-operators-controllers/
├── README.md
├── docs/
│   ├── getting-started.md
│   ├── development-setup.md
│   └── best-practices.md
├── 01-beginner/
│   ├── simple-webapp-operator/
│   │   ├── README.md
│   │   ├── Makefile
│   │   ├── PROJECT
│   │   ├── go.mod
│   │   ├── api/v1alpha1/
│   │   ├── controllers/
│   │   ├── config/
│   │   └── examples/
│   └── configmap-syncer/
│       ├── README.md
│       ├── Makefile
│       ├── PROJECT
│       ├── go.mod
│       ├── api/v1alpha1/
│       ├── controllers/
│       ├── config/
│       └── examples/
├── 02-intermediate/
│   ├── statefulset-backup-operator/
│   │   ├── README.md
│   │   ├── Makefile
│   │   ├── PROJECT
│   │   ├── go.mod
│   │   ├── api/v1alpha1/
│   │   ├── controllers/
│   │   ├── config/
│   │   └── examples/
│   ├── database-user-manager/
│   │   ├── README.md
│   │   ├── Makefile
│   │   ├── PROJECT
│   │   ├── go.mod
│   │   ├── api/v1alpha1/
│   │   ├── controllers/
│   │   ├── config/
│   │   └── examples/
│   └── hpa-custom-metric-operator/
│       ├── README.md
│       ├── Makefile
│       ├── PROJECT
│       ├── go.mod
│       ├── api/v1alpha1/
│       ├── controllers/
│       ├── config/
│       └── examples/
└── 03-advanced/
    ├── cluster-provisioner-operator/
    │   ├── README.md
    │   ├── Makefile
    │   ├── PROJECT
    │   ├── go.mod
    │   ├── api/v1alpha1/
    │   ├── controllers/
    │   ├── config/
    │   └── examples/
    └── rolling-upgrade-operator/
        ├── README.md
        ├── Makefile
        ├── PROJECT
        ├── go.mod
        ├── api/v1alpha1/
        ├── controllers/
        ├── config/
        └── examples/
```

## Beginner Projects

### 1. Simple Web App Operator
**Focus:** Basic reconciliation loop, resource ownership, CRUD operations

Create and manage Deployments and Services based on a custom `WebApp` resource.

**Key Concepts:**
- Basic reconciliation loop
- Resource creation and updates
- Owner references
- Patching resources

**CRD Example:**
```yaml
apiVersion: apps.example.com/v1alpha1
kind: WebApp
metadata:
  name: my-app
spec:
  image: nginx:latest
  replicas: 3
```

[Full Documentation](01-beginner/simple-webapp-operator/README.md)

### 2. ConfigMap Syncer Controller
**Focus:** Cross-namespace operations, watching multiple resources

Synchronize ConfigMaps across multiple namespaces automatically.

**Key Concepts:**
- Watching multiple resources
- Cross-namespace operations
- Finalizers and cleanup
- Handling resource deletion

**CRD Example:**
```yaml
apiVersion: config.example.com/v1alpha1
kind: ConfigMapSyncer
metadata:
  name: sync-app-config
spec:
  sourceNamespace: default
  sourceConfigMap: app-config
  targetNamespaces:
    - dev
    - staging
    - prod
```

[Full Documentation](01-beginner/configmap-syncer/README.md)

## Intermediate Projects

### 3. StatefulSet Backup Operator
**Focus:** Jobs, scheduling, PVC interactions

Schedule and execute backups for StatefulSet persistent volumes.

**Key Concepts:**
- Creating and managing Jobs
- Cron-based scheduling
- PVC/PV interactions
- Status reporting

**CRD Example:**
```yaml
apiVersion: backup.example.com/v1alpha1
kind: BackupPolicy
metadata:
  name: db-backup
spec:
  schedule: "0 2 * * *"
  pvcSelector:
    matchLabels:
      app: postgresql
  backupStrategy: snapshot
```

[Full Documentation](02-intermediate/statefulset-backup-operator/README.md)

### 4. Database User Manager Operator
**Focus:** External system integration, secret management

Manage PostgreSQL database users and permissions declaratively.

**Key Concepts:**
- External API/database integration
- Secret generation and management
- Application-level reconciliation
- Error handling and retries

**CRD Example:**
```yaml
apiVersion: database.example.com/v1alpha1
kind: PostgresUser
metadata:
  name: app-user
spec:
  username: myapp
  database: myapp_db
  privileges:
    - SELECT
    - INSERT
    - UPDATE
```

[Full Documentation](02-intermediate/database-user-manager/README.md)

### 5. HPA Custom Metric Operator
**Focus:** External metrics, custom scaling logic

Scale applications based on external metrics like queue depth.

**Key Concepts:**
- External system integration (queues)
- Custom metrics API
- Scaling decisions
- HPA integration

**CRD Example:**
```yaml
apiVersion: autoscaling.example.com/v1alpha1
kind: ExternalScaler
metadata:
  name: queue-scaler
spec:
  targetDeployment: worker
  metricSource: rabbitmq
  queueName: tasks
  targetQueueDepth: 50
```

[Full Documentation](02-intermediate/hpa-custom-metric-operator/README.md)

## Advanced Projects

### 6. Cluster Provisioner Operator
**Focus:** Infrastructure provisioning, long-running operations

Provision development Kubernetes clusters using Kind or K3s.

**Key Concepts:**
- Running infrastructure tools
- Long-running operations
- Kubeconfig management
- Complex lifecycle management

**CRD Example:**
```yaml
apiVersion: infrastructure.example.com/v1alpha1
kind: DevCluster
metadata:
  name: my-dev-cluster
spec:
  version: v1.28.0
  nodes: 3
  provider: kind
```

[Full Documentation](03-advanced/cluster-provisioner-operator/README.md)

### 7. Application Rolling Upgrade Operator
**Focus:** Day 2 operations, orchestrated upgrades

Orchestrate complex application upgrades with health checks and migrations.

**Key Concepts:**
- Day 2 operations modeling
- Complex state management
- Health checking
- Database migrations
- Rollback logic

**CRD Example:**
```yaml
apiVersion: apps.example.com/v1alpha1
kind: ManagedApplication
metadata:
  name: my-app
spec:
  version: v2.0.0
  paused: false
  upgradeStrategy:
    type: RollingWithMigration
    migrationScript: /scripts/migrate.sh
```

[Full Documentation](03-advanced/rolling-upgrade-operator/README.md)

## Prerequisites

- Go 1.21 or later
- Docker or Podman
- kubectl
- Access to a Kubernetes cluster (local: kind, minikube, k3d)
- Kubebuilder v3.x or Operator SDK v1.x

## Getting Started

1. **Clone the repository:**
   ```bash
   git clone https://github.com/nutcas3/k8s-operators-controllers.git
   cd k8s-operators-controllers
   ```

2. **Choose a project level based on your experience:**
   - New to operators? Start with `01-beginner/simple-webapp-operator`
   - Have basic knowledge? Try `02-intermediate/` projects
   - Ready for complex scenarios? Dive into `03-advanced/`

3. **Follow the project-specific README:**
   Each project has detailed instructions for:
   - Setting up the development environment
   - Building and running the operator
   - Testing with example CRs
   - Deploying to a cluster

4. **Read the documentation:**
   Check out `docs/getting-started.md` for a comprehensive guide.

## Learning Path

We recommend following this progression:

1. **Simple Web App Operator** → Learn reconciliation basics
2. **ConfigMap Syncer** → Master resource watching
3. **Database User Manager** → External integration
4. **StatefulSet Backup Operator** → Jobs and scheduling
5. **HPA Custom Metric Operator** → Advanced scaling
6. **Cluster Provisioner** → Infrastructure management
7. **Rolling Upgrade Operator** → Complex orchestration

## Contributing

Contributions are welcome! Please feel free to:
- Add new project ideas
- Improve existing implementations
- Fix bugs or enhance documentation
- Share your learning experiences

## Resources

- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Operator SDK Documentation](https://sdk.operatorframework.io/)
- [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- [Controller Runtime](https://github.com/kubernetes-sigs/controller-runtime)

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

These projects are designed as learning exercises to build practical Kubernetes operator development skills. Each project includes real-world patterns used in production operators.

---

**Happy Learning!**

Start with the beginner projects and work your way up. Each completed project builds skills for the next level.
