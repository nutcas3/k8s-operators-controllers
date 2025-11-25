# Cluster Provisioner Operator

Provision and manage development Kubernetes clusters using Kind or K3s.

## 📚 Learning Objectives

- ✅ Infrastructure provisioning
- ✅ Long-running operations
- ✅ Kubeconfig management
- ✅ Complex lifecycle management
- ✅ External tool integration
- ✅ Multi-cluster management

## 🎯 What This Operator Does

Watches for `DevCluster` resources and:

1. Provisions Kubernetes clusters using Kind or K3s
2. Manages cluster lifecycle (create, update, delete)
3. Stores kubeconfig in Secrets
4. Monitors cluster health
5. Handles cluster upgrades
6. Manages cluster networking and storage

## 📋 Prerequisites

- Go 1.21+, Docker, kubectl
- kind or k3d installed
- Kubebuilder v3.x

## 🚀 Quick Start

### 1. Create a DevCluster

```bash
kubectl apply -f - <<EOF
apiVersion: infrastructure.example.com/v1alpha1
kind: DevCluster
metadata:
  name: my-dev-cluster
spec:
  version: v1.28.0
  nodes: 3
  provider: kind
  config:
    networking:
      podSubnet: "10.244.0.0/16"
      serviceSubnet: "10.96.0.0/12"
EOF
```

### 2. Access the Cluster

```bash
# Get kubeconfig
kubectl get secret my-dev-cluster-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > /tmp/dev-kubeconfig

# Use the cluster
export KUBECONFIG=/tmp/dev-kubeconfig
kubectl get nodes
```

## 📖 Key Code Snippets

### CRD Definition

```go
type DevClusterSpec struct {
    Version  string `json:"version"`
    Nodes    int32  `json:"nodes"`
    Provider string `json:"provider"` // kind, k3s
    Config   ClusterConfig `json:"config,omitempty"`
}

type DevClusterStatus struct {
    Phase      string             `json:"phase"` // Pending, Provisioning, Ready, Failed
    Kubeconfig string             `json:"kubeconfig,omitempty"`
    Endpoint   string             `json:"endpoint,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### Cluster Provisioning

```go
func (r *Reconciler) provisionKindCluster(ctx context.Context, cluster *DevCluster) error {
    configPath := fmt.Sprintf("/tmp/%s-config.yaml", cluster.Name)
    
    // Generate kind config
    config := generateKindConfig(cluster)
    if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
        return err
    }
    
    // Create cluster
    cmd := exec.CommandContext(ctx, "kind", "create", "cluster",
        "--name", cluster.Name,
        "--config", configPath,
        "--image", fmt.Sprintf("kindest/node:%s", cluster.Spec.Version))
    
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("failed to create cluster: %s", output)
    }
    
    // Get kubeconfig
    return r.extractKubeconfig(ctx, cluster)
}
```

## 🎓 Exercises

1. **Add Ingress Support** - Install ingress controllers
2. **Cluster Templates** - Support cluster templates
3. **Resource Quotas** - Implement cluster resource limits
4. **Monitoring Integration** - Auto-install monitoring stack

## 🔗 Next Steps

- [Rolling Upgrade Operator](../rolling-upgrade-operator/README.md)

---

**Amazing work on infrastructure management!** 🎉
