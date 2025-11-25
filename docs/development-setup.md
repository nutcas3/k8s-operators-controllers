# Development Setup Guide

This guide covers setting up your development environment for building Kubernetes operators.

## Table of Contents

- [System Requirements](#system-requirements)
- [Installing Prerequisites](#installing-prerequisites)
- [Setting Up Your Kubernetes Cluster](#setting-up-your-kubernetes-cluster)
- [IDE Setup](#ide-setup)
- [Troubleshooting](#troubleshooting)

## System Requirements

### Minimum Requirements

- **CPU**: 2 cores
- **RAM**: 4 GB (8 GB recommended)
- **Disk**: 20 GB free space
- **OS**: Linux, macOS, or Windows with WSL2

### Software Versions

- Go 1.21+
- Kubernetes 1.24+
- Kubebuilder 3.x
- Docker 20.10+ or Podman 3.0+

## Installing Prerequisites

### 1. Install Go

#### macOS
```bash
brew install go
```

#### Linux
```bash
# Download and install
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

# Add to PATH (add to ~/.bashrc or ~/.zshrc)
export PATH=$PATH:/usr/local/go/bin
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
```

#### Windows (WSL2)
```bash
# Same as Linux instructions above
```

**Verify Installation:**
```bash
go version
# Should output: go version go1.21.5 ...
```

### 2. Install Docker

#### macOS
```bash
brew install --cask docker
# Or download Docker Desktop from docker.com
```

#### Linux (Ubuntu/Debian)
```bash
# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Add your user to docker group
sudo usermod -aG docker $USER
newgrp docker

# Enable Docker service
sudo systemctl enable docker
sudo systemctl start docker
```

#### Alternative: Podman
```bash
# macOS
brew install podman

# Linux
sudo apt-get install podman  # Debian/Ubuntu
sudo dnf install podman      # Fedora
```

**Verify Installation:**
```bash
docker version
# or
podman version
```

### 3. Install kubectl

#### macOS
```bash
brew install kubectl
```

#### Linux
```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

**Verify Installation:**
```bash
kubectl version --client
```

### 4. Install Kubebuilder

#### macOS/Linux
```bash
# Download and install
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
chmod +x kubebuilder
sudo mv kubebuilder /usr/local/bin/
```

**Verify Installation:**
```bash
kubebuilder version
```

### 5. Install kind (Kubernetes in Docker)

```bash
# Install kind
go install sigs.k8s.io/kind@latest

# Verify installation
kind version
```

### 6. Install Additional Tools

#### kustomize
```bash
# macOS
brew install kustomize

# Linux
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh" | bash
sudo mv kustomize /usr/local/bin/
```

#### controller-gen (installed automatically by Kubebuilder, but can be installed separately)
```bash
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
```

## Setting Up Your Kubernetes Cluster

### Option 1: kind (Recommended for Development)

#### Create a Basic Cluster
```bash
kind create cluster --name operator-dev
```

#### Create a Multi-Node Cluster
```bash
cat <<EOF | kind create cluster --name operator-dev --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF
```

#### Verify Cluster
```bash
kubectl cluster-info --context kind-operator-dev
kubectl get nodes
```

#### Delete Cluster (when done)
```bash
kind delete cluster --name operator-dev
```

### Option 2: minikube

```bash
# Start minikube
minikube start --cpus=2 --memory=4096

# Verify
kubectl get nodes

# Stop minikube
minikube stop

# Delete minikube
minikube delete
```

### Option 3: k3d

```bash
# Create cluster
k3d cluster create operator-dev --agents 2

# Verify
kubectl get nodes

# Delete cluster
k3d cluster delete operator-dev
```

### Configure kubectl Context

```bash
# List contexts
kubectl config get-contexts

# Switch context
kubectl config use-context kind-operator-dev

# Set default namespace (optional)
kubectl config set-context --current --namespace=default
```

## IDE Setup

### Visual Studio Code

#### Recommended Extensions

1. **Go** (golang.go)
   - Go language support
   - IntelliSense, debugging, formatting

2. **Kubernetes** (ms-kubernetes-tools.vscode-kubernetes-tools)
   - Kubernetes manifest support
   - Cluster management

3. **YAML** (redhat.vscode-yaml)
   - YAML validation and auto-completion
   - Kubernetes schema support

4. **Go Test Explorer** (premparihar.gotestexplorer)
   - Run and debug tests

#### Settings Configuration

Create `.vscode/settings.json` in your project:

```json
{
  "go.useLanguageServer": true,
  "go.lintTool": "golangci-lint",
  "go.lintOnSave": "package",
  "go.formatTool": "goimports",
  "go.testFlags": ["-v"],
  "[go]": {
    "editor.formatOnSave": true,
    "editor.codeActionsOnSave": {
      "source.organizeImports": true
    }
  },
  "yaml.schemas": {
    "kubernetes": "*.yaml"
  }
}
```

#### Launch Configuration

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Operator",
      "type": "go",
      "request": "launch",
      "mode": "auto",
      "program": "${workspaceFolder}/cmd/main.go",
      "env": {},
      "args": []
    }
  ]
}
```

### GoLand / IntelliJ IDEA

1. Install Go plugin
2. Configure Go SDK (Preferences → Go → GOROOT)
3. Enable Go modules (automatic in recent versions)
4. Install Kubernetes plugin for YAML support

### Vim/Neovim

```vim
" Install vim-go
Plug 'fatih/vim-go'
Plug 'neoclide/coc.nvim'

" Configure vim-go
let g:go_fmt_command = "goimports"
let g:go_auto_type_info = 1
let g:go_def_mode='gopls'
let g:go_info_mode='gopls'
```

## Development Workflow

### 1. Create a New Operator Project

```bash
# Create project directory
mkdir my-operator
cd my-operator

# Initialize with Kubebuilder
kubebuilder init --domain example.com --repo github.com/myuser/my-operator

# Create an API
kubebuilder create api --group apps --version v1alpha1 --kind MyResource
```

### 2. Development Cycle

```bash
# 1. Edit your types (api/v1alpha1/*_types.go)
# 2. Generate manifests and code
make manifests generate

# 3. Install CRDs
make install

# 4. Run operator locally
make run

# 5. Test with sample resources (in another terminal)
kubectl apply -f config/samples/

# 6. Build and test
make test

# 7. Build Docker image
make docker-build IMG=myregistry/my-operator:v0.1.0
```

### 3. Useful Make Targets

```bash
make help              # Show all available targets
make manifests         # Generate CRD manifests
make generate          # Generate code (DeepCopy, etc.)
make fmt               # Run go fmt
make vet               # Run go vet
make test              # Run tests
make build             # Build manager binary
make run               # Run locally
make install           # Install CRDs
make uninstall         # Uninstall CRDs
make deploy            # Deploy to cluster
make undeploy          # Remove from cluster
make docker-build      # Build Docker image
make docker-push       # Push Docker image
```

## Troubleshooting

### Common Issues

#### Issue: "command not found: kubebuilder"

**Solution:**
```bash
# Ensure kubebuilder is in PATH
echo $PATH | grep -q "/usr/local/bin" || export PATH=$PATH:/usr/local/bin

# Or reinstall
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
chmod +x kubebuilder
sudo mv kubebuilder /usr/local/bin/
```

#### Issue: "Cannot connect to the Docker daemon"

**Solution:**
```bash
# Start Docker service
sudo systemctl start docker

# Or add user to docker group
sudo usermod -aG docker $USER
newgrp docker
```

#### Issue: "The connection to the server localhost:8080 was refused"

**Solution:**
```bash
# Ensure cluster is running
kind get clusters
# or
minikube status

# Set correct context
kubectl config use-context kind-operator-dev
```

#### Issue: "go: cannot find main module"

**Solution:**
```bash
# Initialize Go module
go mod init github.com/myuser/my-operator

# Or ensure you're in the project directory
cd /path/to/your/operator
```

#### Issue: CRD installation fails

**Solution:**
```bash
# Regenerate manifests
make manifests

# Verify CRD files exist
ls config/crd/bases/

# Try manual installation
kubectl apply -f config/crd/bases/
```

### Getting More Help

- Check logs: `kubectl logs -n <namespace> <pod-name>`
- Describe resources: `kubectl describe <resource-type> <resource-name>`
- Check operator logs when running locally
- Review Kubebuilder documentation
- Search GitHub issues in controller-runtime and kubebuilder repos

## Next Steps

Now that your environment is set up:

1. Read [Getting Started Guide](getting-started.md)
2. Try the [Simple Web App Operator](../01-beginner/simple-webapp-operator/README.md)
3. Review [Best Practices](best-practices.md)

---

**Environment ready!** Start building your first operator! 🚀
