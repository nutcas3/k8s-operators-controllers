# Database User Manager Operator

Manage PostgreSQL database users and permissions declaratively through Kubernetes.

## Learning Objectives

- External system integration (PostgreSQL)
- Secret generation and management
- Application-level reconciliation
- Error handling and retries
- Credential rotation
- Connection pooling

## What This Operator Does

Watches for `PostgresUser` custom resources and:

1. Creates database users in PostgreSQL
2. Grants specified permissions
3. Generates and stores credentials in Secrets
4. Handles password rotation
5. Cleans up users when resources are deleted
6. Reports connection status

## Prerequisites

- Go 1.21+, Docker, kubectl, Kubernetes cluster
- PostgreSQL database (for testing)
- Kubebuilder v3.x

## Quick Start

### 1. Deploy PostgreSQL for Testing

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: postgres-admin
stringData:
  username: postgres
  password: adminpass
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
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
          valueFrom:
            secretKeyRef:
              name: postgres-admin
              key: password
        ports:
        - containerPort: 5432
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector:
    app: postgres
  ports:
  - port: 5432
EOF
```

### 2. Create a PostgresUser

```bash
kubectl apply -f - <<EOF
apiVersion: database.example.com/v1alpha1
kind: PostgresUser
metadata:
  name: app-user
spec:
  username: myapp
  database: myapp_db
  host: postgres.default.svc.cluster.local
  adminSecretRef:
    name: postgres-admin
  privileges:
    - SELECT
    - INSERT
    - UPDATE
    - DELETE
  secretName: myapp-db-credentials
EOF
```

### 3. Verify User Creation

```bash
# Check PostgresUser status
kubectl get postgresuser app-user

# Verify secret was created
kubectl get secret myapp-db-credentials

# Test connection
kubectl run -it --rm psql --image=postgres:15 --restart=Never -- \
  psql -h postgres -U myapp -d myapp_db
```

## 📖 Key Code Snippets

### CRD Definition

```go
type PostgresUserSpec struct {
    Username       string              `json:"username"`
    Database       string              `json:"database"`
    Host           string              `json:"host"`
    AdminSecretRef corev1.SecretReference `json:"adminSecretRef"`
    Privileges     []string            `json:"privileges"`
    SecretName     string              `json:"secretName"`
}

type PostgresUserStatus struct {
    Ready      bool               `json:"ready"`
    Message    string             `json:"message,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

### Database Connection

```go
import "github.com/lib/pq"

func (r *Reconciler) connectToDatabase(ctx context.Context, user *PostgresUser) (*sql.DB, error) {
    // Get admin credentials
    secret := &corev1.Secret{}
    if err := r.Get(ctx, types.NamespacedName{
        Name: user.Spec.AdminSecretRef.Name,
        Namespace: user.Namespace,
    }, secret); err != nil {
        return nil, err
    }
    
    connStr := fmt.Sprintf("host=%s user=%s password=%s dbname=postgres sslmode=disable",
        user.Spec.Host,
        string(secret.Data["username"]),
        string(secret.Data["password"]))
    
    return sql.Open("postgres", connStr)
}
```

### User Creation

```go
func (r *Reconciler) createDatabaseUser(ctx context.Context, db *sql.DB, user *PostgresUser) error {
    password := generatePassword(32)
    
    // Create user
    _, err := db.ExecContext(ctx, fmt.Sprintf(
        "CREATE USER %s WITH PASSWORD '%s'",
        pq.QuoteIdentifier(user.Spec.Username),
        password))
    if err != nil && !strings.Contains(err.Error(), "already exists") {
        return err
    }
    
    // Grant privileges
    for _, priv := range user.Spec.Privileges {
        _, err := db.ExecContext(ctx, fmt.Sprintf(
            "GRANT %s ON ALL TABLES IN SCHEMA public TO %s",
            priv, pq.QuoteIdentifier(user.Spec.Username)))
        if err != nil {
            return err
        }
    }
    
    // Store credentials in Secret
    return r.createOrUpdateSecret(ctx, user, password)
}
```

## Exercises

1. Add Role Support - Support PostgreSQL roles
2. Implement Password Rotation - Automatic credential rotation
3. Support Multiple Databases - Manage users across databases
4. Add MySQL Support - Extend to support MySQL

## Next Steps

- [HPA Custom Metric Operator](../hpa-custom-metric-operator/README.md)
- [Cluster Provisioner Operator](../../03-advanced/cluster-provisioner-operator/README.md)

---

**Great job integrating with external systems!** 🎉
