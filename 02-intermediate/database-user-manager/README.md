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

1. Creates database users in PostgreSQL or MySQL (`spec.engine`)
2. Grants specified permissions across one or more databases
3. Grants existing database roles (`spec.roles`)
4. Generates and stores credentials in Secrets
5. Handles manual (`spec.rotatePassword`) and scheduled (`spec.rotationInterval`) password rotation
6. Cleans up users when resources are deleted
7. Reports connection status

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
    Username         string                 `json:"username"`
    Database         string                 `json:"database"`
    Databases        []string               `json:"databases,omitempty"`
    Engine           string                 `json:"engine,omitempty"` // postgres | mysql
    Host             string                 `json:"host"`
    Port             int32                  `json:"port,omitempty"`
    AdminSecretRef   corev1.SecretReference `json:"adminSecretRef"`
    Privileges       []string               `json:"privileges"`
    Roles            []string               `json:"roles,omitempty"`
    SecretName       string                 `json:"secretName"`
    RotatePassword   bool                   `json:"rotatePassword,omitempty"`
    RotationInterval *metav1.Duration       `json:"rotationInterval,omitempty"`
}

type PostgresUserStatus struct {
    Ready                    bool               `json:"ready"`
    Message                  string             `json:"message,omitempty"`
    LastPasswordRotation     *metav1.Time       `json:"lastPasswordRotation,omitempty"`
    ObservedRotatePassword   bool               `json:"observedRotatePassword,omitempty"`
    Conditions               []metav1.Condition `json:"conditions,omitempty"`
}
```

### Engine Abstraction

`controllers/dbengine.go` defines a `dbEngine` interface so the reconciler can
support both PostgreSQL and MySQL:

```go
type dbEngine interface {
    Driver() string
    DefaultPort() int32
    AdminDatabase() string
    DSN(host string, port int32, user, password, database string) string
    UserExists(ctx context.Context, db *sql.DB, username string) (bool, error)
    CreateUserSQL(username, password string) string
    SetPasswordSQL(username, password string) string
    DropUserSQL(username string) string
    DatabaseGrantSQL(database, username string) string
    PrivilegeGrantSQL(privilege, database, username string) []string
    RoleGrantSQL(role, username string) string
    ValidPrivilege(privilege string) bool
}
```

Each engine owns its DSN format, identifier quoting, and privilege allowlist —
privileges can't be parameterized in `GRANT`, so they're validated before
interpolation.

### Database Connection

```go
func (r *PostgresUserReconciler) connect(ctx context.Context, eng dbEngine,
    user *PostgresUser, adminUser, adminPassword, database string) (*sql.DB, error) {

    port := user.Spec.Port
    if port == 0 {
        port = eng.DefaultPort()
    }

    db, err := sql.Open(eng.Driver(), eng.DSN(user.Spec.Host, port, adminUser, adminPassword, database))
    if err != nil {
        return nil, err
    }
    if err := db.PingContext(ctx); err != nil {
        db.Close()
        return nil, err
    }
    return db, nil
}
```

Admin credentials are read once per reconcile from `spec.adminSecretRef` and
reused for every connection — including the per-database connections needed
for table-level grants.

### User Creation and Rotation

```go
// Rotate when the user is missing, the secret is gone, the manual
// toggle changed, or the rotation interval elapsed
rotate := !exists || !secretFound ||
    user.Spec.RotatePassword != user.Status.ObservedRotatePassword ||
    rotationDue(user)

if rotate {
    password = generatePassword(32)

    stmt := eng.CreateUserSQL(user.Spec.Username, password)
    if exists {
        stmt = eng.SetPasswordSQL(user.Spec.Username, password)
    }
    if _, err := db.ExecContext(ctx, stmt); err != nil {
        return err
    }

    now := metav1.Now()
    user.Status.LastPasswordRotation = &now
}
```

`status.observedRotatePassword` is only persisted after the new credentials
Secret is written, so a failed reconcile retries the rotation instead of
desyncing the Secret from the database.

With `spec.rotationInterval` set, the reconciler returns
`ctrl.Result{RequeueAfter: nextRotationIn(user)}` so rotation happens on
schedule without an external cron.

## Implemented Extensions

1. **Role support** — `spec.roles` grants existing database roles to the user
   (`GRANT role TO user` / MySQL 8 `GRANT 'role'@'%' TO 'user'@'%'`).
2. **Automatic password rotation** — `spec.rotationInterval` schedules
   rotation via `RequeueAfter`; toggling `spec.rotatePassword` still triggers
   a manual rotation exactly once.
3. **Multiple databases** — `spec.databases` grants the same privileges on
   additional databases alongside `spec.database`.
4. **MySQL support** — `spec.engine: mysql` uses the `go-sql-driver/mysql`
   driver and MySQL account/grant syntax (`'user'@'%'`, `GRANT ... ON db.*`).

## Further Exercises

1. Use TLS connections (`sslmode` / `tls=` DSN options) instead of `disable`
2. Revoke privileges removed from `spec.privileges` (diff-based reconciliation)
3. Pool admin connections per host instead of opening per reconcile
4. Emit Kubernetes Events on rotation and grant failures

## Next Steps

- [HPA Custom Metric Operator](../hpa-custom-metric-operator/README.md)
- [Cluster Provisioner Operator](../../03-advanced/cluster-provisioner-operator/README.md)

---

**Great job integrating with external systems!** 🎉
