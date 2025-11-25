# Getting Started with Kubernetes Operators

This guide will help you get started with building Kubernetes operators using this learning repository.

## Table of Contents

- [What are Kubernetes Operators?](#what-are-kubernetes-operators)
- [Prerequisites](#prerequisites)
- [Setting Up Your Environment](#setting-up-your-environment)
- [Understanding the Basics](#understanding-the-basics)
- [Your First Operator](#your-first-operator)
- [Next Steps](#next-steps)

## What are Kubernetes Operators?

Kubernetes Operators are software extensions that use custom resources to manage applications and their components. Operators follow Kubernetes principles, notably the control loop pattern.

### Key Concepts

**Custom Resource Definitions (CRDs)**
- Extend the Kubernetes API with your own resource types
- Define the schema for your custom resources
- Example: `WebApp`, `BackupPolicy`, `PostgresUser`

**Controllers**
- Watch for changes to resources
- Reconcile the actual state with the desired state
- Implement the business logic of your operator

**Reconciliation Loop**
- The core pattern: observe → analyze → act
- Continuously ensures desired state matches actual state
- Handles creation, updates, and deletion of resources

## Prerequisites

Before starting, ensure you have:

### Required Tools

1. **Go** (1.21 or later)
   ```bash
   go version
   ```

2. **Docker** or **Podman**
   ```bash
   docker --version
   # or
   podman --version
   ```

3. **kubectl**
   ```bash
   kubectl version --client
   ```

4. **Kubebuilder** (v3.x)
   ```bash
   # Install Kubebuilder
   curl -L -o kubebuilder https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)
   chmod +x kubebuilder && mv kubebuilder /usr/local/bin/
   ```

5. **A Kubernetes Cluster**
   - **kind** (recommended for local development)
     ```bash
     # Install kind
     go install sigs.k8s.io/kind@latest
     
     # Create a cluster
     kind create cluster --name operator-dev
     ```
   - **minikube**
     ```bash
     minikube start
     ```
   - **k3d**
     ```bash
     k3d cluster create operator-dev
     ```

### Recommended Knowledge

- Basic understanding of Kubernetes concepts (Pods, Deployments, Services)
- Familiarity with Go programming language
- Understanding of YAML and JSON
- Basic command-line proficiency

## Setting Up Your Environment

### 1. Verify Your Kubernetes Cluster

```bash
kubectl cluster-info
kubectl get nodes
```

### 2. Set Up Your Workspace

```bash
# Clone the repository
git clone https://github.com/nutcas3/k8s-operators-controllers.git
cd k8s-operators-controllers
```

### 3. Verify Kubebuilder Installation

```bash
kubebuilder version
```

You should see output similar to:
```
Version: main.version{KubeBuilderVersion:"3.x.x", ...}
```

## Understanding the Basics

### The Operator Pattern

Operators automate the management of complex applications by encoding operational knowledge into software. They:

1. **Observe** - Watch for changes to custom resources
2. **Analyze** - Determine what actions are needed
3. **Act** - Create, update, or delete Kubernetes resources

### Anatomy of an Operator

```
operator/
├── api/v1alpha1/          # CRD definitions
│   └── webapp_types.go    # Your custom resource schema
├── controllers/           # Reconciliation logic
│   └── webapp_controller.go
├── config/               # Kubernetes manifests
│   ├── crd/             # Generated CRD YAML
│   ├── rbac/            # RBAC permissions
│   └── samples/         # Example custom resources
├── main.go              # Entry point
└── Makefile             # Build and deployment commands
```

### The Reconciliation Loop

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch the custom resource
    var myResource MyResourceType
    if err := r.Get(ctx, req.NamespacedName, &myResource); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }
    
    // 2. Reconcile logic - ensure desired state
    // - Create missing resources
    // - Update existing resources
    // - Delete resources that shouldn't exist
    
    // 3. Update status
    myResource.Status.Condition = "Ready"
    r.Status().Update(ctx, &myResource)
    
    // 4. Return result
    return ctrl.Result{}, nil
}
```

## Your First Operator

### Start with the Simple Web App Operator

This is the perfect first project to understand operator basics.

```bash
cd 01-beginner/simple-webapp-operator
```

### What You'll Learn

1. **Creating a CRD** - Define a `WebApp` custom resource
2. **Building a Controller** - Implement reconciliation logic
3. **Managing Resources** - Create Deployments and Services
4. **Owner References** - Establish resource relationships
5. **Testing** - Verify your operator works correctly

### Step-by-Step Process

1. **Explore the API definition**
   ```bash
   cat api/v1alpha1/webapp_types.go
   ```

2. **Understand the controller**
   ```bash
   cat controllers/webapp_controller.go
   ```

3. **Install the CRD**
   ```bash
   make install
   ```

4. **Run the operator locally**
   ```bash
   make run
   ```

5. **Create a sample resource** (in another terminal)
   ```bash
   kubectl apply -f config/samples/apps_v1alpha1_webapp.yaml
   ```

6. **Verify it works**
   ```bash
   kubectl get webapps
   kubectl get deployments
   kubectl get services
   ```

### Common Commands

```bash
# Generate CRD manifests
make manifests

# Install CRDs into the cluster
make install

# Run operator locally (for development)
make run

# Build and push Docker image
make docker-build docker-push IMG=<your-registry>/webapp-operator:tag

# Deploy operator to cluster
make deploy IMG=<your-registry>/webapp-operator:tag

# Uninstall CRDs
make uninstall

# Undeploy operator
make undeploy
```

## Next Steps

### Learning Path

1. ✅ **Simple Web App Operator** - You are here!
2. **ConfigMap Syncer** - Learn about watching multiple resources
3. **Database User Manager** - Integrate with external systems
4. **StatefulSet Backup Operator** - Work with Jobs and scheduling
5. **HPA Custom Metric Operator** - Advanced scaling patterns
6. **Cluster Provisioner** - Infrastructure management
7. **Rolling Upgrade Operator** - Complex orchestration

### Additional Resources

- 📖 [Kubebuilder Book](https://book.kubebuilder.io/) - Comprehensive guide
- 📖 [Operator SDK Documentation](https://sdk.operatorframework.io/) - Alternative framework
- 📖 [Controller Runtime Docs](https://pkg.go.dev/sigs.k8s.io/controller-runtime) - Core library
- 📖 [Kubernetes API Conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)

### Best Practices

- Start simple and iterate
- Write tests for your controllers
- Use proper error handling and logging
- Implement status conditions properly
- Follow Kubernetes API conventions
- Use finalizers for cleanup logic

### Getting Help

- Check project-specific README files
- Review the `docs/best-practices.md` guide
- Explore example implementations in each project
- Read the Kubebuilder documentation
- Join the Kubernetes Slack (#kubebuilder channel)

---

**Ready to build your first operator?** Head to [01-beginner/simple-webapp-operator](../01-beginner/simple-webapp-operator/README.md) and start coding!
