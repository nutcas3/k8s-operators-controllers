package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	infrav1alpha1 "github.com/nutcas3/cluster-provisioner-operator/api/v1alpha1"
)

// provisionTimeout bounds long-running provider operations.
const provisionTimeout = 10 * time.Minute

// clusterProvider abstracts the external CLI used to manage dev clusters
// (kind for provider=kind, k3d for provider=k3s).
type clusterProvider interface {
	// Exists reports whether a cluster with the given name exists.
	Exists(ctx context.Context, name string) (bool, error)
	// Create provisions the cluster. May block for several minutes.
	Create(ctx context.Context, cluster *infrav1alpha1.DevCluster) error
	// Delete removes the cluster.
	Delete(ctx context.Context, name string) error
	// Kubeconfig returns a kubeconfig for the cluster.
	Kubeconfig(ctx context.Context, name string) ([]byte, error)
}

func providerFor(name string) (clusterProvider, error) {
	switch name {
	case "kind":
		return kindProvider{}, nil
	case "k3s":
		return k3dProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", name)
	}
}

// runCLI executes a provider binary, returning combined output on error so
// failures surface in status/events.
func runCLI(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, provisionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

// kindProvider manages clusters via `kind`.
type kindProvider struct{}

func (kindProvider) Exists(ctx context.Context, name string) (bool, error) {
	out, err := runCLI(ctx, "kind", "get", "clusters")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true, nil
		}
	}
	return false, nil
}

func (kindProvider) Create(ctx context.Context, cluster *infrav1alpha1.DevCluster) error {
	configPath := filepath.Join(os.TempDir(), fmt.Sprintf("kind-%s-config.yaml", cluster.Name))
	defer os.Remove(configPath)

	if err := os.WriteFile(configPath, []byte(kindConfig(cluster)), 0o644); err != nil {
		return err
	}

	_, err := runCLI(ctx, "kind", "create", "cluster",
		"--name", cluster.Name,
		"--config", configPath,
		"--image", fmt.Sprintf("kindest/node:%s", cluster.Spec.Version))
	return err
}

func (kindProvider) Delete(ctx context.Context, name string) error {
	_, err := runCLI(ctx, "kind", "delete", "cluster", "--name", name)
	return err
}

func (kindProvider) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	return runCLI(ctx, "kind", "get", "kubeconfig", "--name", name)
}

// kindConfig renders a kind cluster config with one control-plane node and
// spec.nodes workers.
func kindConfig(cluster *infrav1alpha1.DevCluster) string {
	var b strings.Builder
	b.WriteString("kind: Cluster\napiVersion: kind.x-k8s.io/v1alpha4\n")

	if net := cluster.Spec.Config.Networking; net.PodSubnet != "" || net.ServiceSubnet != "" {
		b.WriteString("networking:\n")
		if net.PodSubnet != "" {
			fmt.Fprintf(&b, "  podSubnet: %q\n", net.PodSubnet)
		}
		if net.ServiceSubnet != "" {
			fmt.Fprintf(&b, "  serviceSubnet: %q\n", net.ServiceSubnet)
		}
	}

	b.WriteString("nodes:\n- role: control-plane\n")
	for i := int32(0); i < cluster.Spec.Nodes; i++ {
		b.WriteString("- role: worker\n")
	}
	return b.String()
}

// k3dProvider manages k3s clusters via `k3d`.
type k3dProvider struct{}

func (k3dProvider) Exists(ctx context.Context, name string) (bool, error) {
	out, err := runCLI(ctx, "k3d", "cluster", "list", "-o", "json")
	if err != nil {
		return false, err
	}
	var clusters []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &clusters); err != nil {
		return false, fmt.Errorf("parsing k3d cluster list: %w", err)
	}
	for _, c := range clusters {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}

func (k3dProvider) Create(ctx context.Context, cluster *infrav1alpha1.DevCluster) error {
	// k3s image tags look like v1.28.0-k3s1
	image := fmt.Sprintf("rancher/k3s:%s-k3s1", cluster.Spec.Version)

	args := []string{"cluster", "create", cluster.Name,
		"--servers", "1",
		"--agents", fmt.Sprintf("%d", cluster.Spec.Nodes),
		"--image", image}
	_, err := runCLI(ctx, "k3d", args...)
	return err
}

func (k3dProvider) Delete(ctx context.Context, name string) error {
	_, err := runCLI(ctx, "k3d", "cluster", "delete", name)
	return err
}

func (k3dProvider) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	return runCLI(ctx, "k3d", "kubeconfig", "get", name)
}
