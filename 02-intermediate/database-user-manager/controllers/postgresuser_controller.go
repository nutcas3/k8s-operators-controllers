package controllers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	_ "github.com/lib/pq"
	databasev1alpha1 "github.com/nutcas3/database-user-manager/api/v1alpha1"
)

const (
	finalizerName = "postgresuser.database.example.com/finalizer"
)

// PostgresUserReconciler reconciles a PostgresUser object
type PostgresUserReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=database.example.com,resources=postgresusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=database.example.com,resources=postgresusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=database.example.com,resources=postgresusers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch

func (r *PostgresUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the PostgresUser
	user := &databasev1alpha1.PostgresUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if !user.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, user)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(user, finalizerName) {
		controllerutil.AddFinalizer(user, finalizerName)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Connect to database
	db, err := r.connectToDatabase(ctx, user)
	if err != nil {
		log.Error(err, "Failed to connect to database")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Connection failed: %v", err))
		return ctrl.Result{}, err
	}
	defer db.Close()

	// Check if user exists
	exists, err := r.userExists(ctx, db, user)
	if err != nil {
		log.Error(err, "Failed to check if user exists")
		return ctrl.Result{}, err
	}

	var password string
	if !exists || user.Spec.RotatePassword {
		// Create or update user
		password, err = r.createOrUpdateUser(ctx, db, user)
		if err != nil {
			log.Error(err, "Failed to create/update user")
			r.updateStatus(ctx, user, false, fmt.Sprintf("User creation failed: %v", err))
			return ctrl.Result{}, err
		}

		// Update password rotation timestamp
		now := metav1.Now()
		user.Status.LastPasswordRotation = &now
	} else {
		// Get existing password from secret
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      user.Spec.SecretName,
			Namespace: user.Namespace,
		}, secret); err == nil {
			password = string(secret.Data["password"])
		}
	}

	// Grant privileges
	if err := r.grantPrivileges(ctx, db, user); err != nil {
		log.Error(err, "Failed to grant privileges")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Privilege grant failed: %v", err))
		return ctrl.Result{}, err
	}

	// Create or update secret with credentials
	if password != "" {
		if err := r.createOrUpdateSecret(ctx, user, password); err != nil {
			log.Error(err, "Failed to create/update secret")
			return ctrl.Result{}, err
		}
	}

	// Update status
	r.updateStatus(ctx, user, true, "User ready")

	log.Info("Successfully reconciled PostgresUser")
	return ctrl.Result{}, nil
}

func (r *PostgresUserReconciler) handleDeletion(ctx context.Context, user *databasev1alpha1.PostgresUser) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(user, finalizerName) {
		// Connect to database
		db, err := r.connectToDatabase(ctx, user)
		if err != nil {
			log.Error(err, "Failed to connect to database for cleanup")
			// Continue with finalizer removal even if connection fails
		} else {
			defer db.Close()

			// Drop user
			if err := r.dropUser(ctx, db, user); err != nil {
				log.Error(err, "Failed to drop user")
				// Continue with finalizer removal
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(user, finalizerName)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *PostgresUserReconciler) connectToDatabase(ctx context.Context, user *databasev1alpha1.PostgresUser) (*sql.DB, error) {
	// Get admin credentials
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      user.Spec.AdminSecretRef.Name,
		Namespace: user.Namespace,
	}, secret); err != nil {
		return nil, fmt.Errorf("failed to get admin secret: %w", err)
	}

	port := user.Spec.Port
	if port == 0 {
		port = 5432
	}

	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable",
		user.Spec.Host,
		port,
		string(secret.Data["username"]),
		string(secret.Data["password"]))

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func (r *PostgresUserReconciler) userExists(ctx context.Context, db *sql.DB, user *databasev1alpha1.PostgresUser) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)"
	err := db.QueryRowContext(ctx, query, user.Spec.Username).Scan(&exists)
	return exists, err
}

func (r *PostgresUserReconciler) createOrUpdateUser(ctx context.Context, db *sql.DB, user *databasev1alpha1.PostgresUser) (string, error) {
	password := generatePassword(32)

	exists, err := r.userExists(ctx, db, user)
	if err != nil {
		return "", err
	}

	if exists {
		// Update password
		query := fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s'",
			quoteIdentifier(user.Spec.Username),
			password)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return "", err
		}
	} else {
		// Create user
		query := fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'",
			quoteIdentifier(user.Spec.Username),
			password)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return "", err
		}
	}

	return password, nil
}

func (r *PostgresUserReconciler) grantPrivileges(ctx context.Context, db *sql.DB, user *databasev1alpha1.PostgresUser) error {
	// Grant database access
	query := fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s",
		quoteIdentifier(user.Spec.Database),
		quoteIdentifier(user.Spec.Username))
	if _, err := db.ExecContext(ctx, query); err != nil {
		return err
	}

	// Connect to target database
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		user.Spec.Host,
		user.Spec.Port,
		"postgres", // Use admin user
		"",         // Would need admin password
		user.Spec.Database)

	targetDB, err := sql.Open("postgres", connStr)
	if err != nil {
		return err
	}
	defer targetDB.Close()

	// Grant privileges on all tables
	for _, priv := range user.Spec.Privileges {
		query := fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA public TO %s",
			priv, quoteIdentifier(user.Spec.Username))
		if _, err := targetDB.ExecContext(ctx, query); err != nil {
			return err
		}

		// Grant on future tables
		query = fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT %s ON TABLES TO %s",
			priv, quoteIdentifier(user.Spec.Username))
		if _, err := targetDB.ExecContext(ctx, query); err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresUserReconciler) dropUser(ctx context.Context, db *sql.DB, user *databasev1alpha1.PostgresUser) error {
	query := fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdentifier(user.Spec.Username))
	_, err := db.ExecContext(ctx, query)
	return err
}

func (r *PostgresUserReconciler) createOrUpdateSecret(ctx context.Context, user *databasev1alpha1.PostgresUser, password string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      user.Spec.SecretName,
			Namespace: user.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if secret.Data == nil {
			secret.Data = make(map[string][]byte)
		}
		secret.Data["username"] = []byte(user.Spec.Username)
		secret.Data["password"] = []byte(password)
		secret.Data["host"] = []byte(user.Spec.Host)
		secret.Data["port"] = []byte(fmt.Sprintf("%d", user.Spec.Port))
		secret.Data["database"] = []byte(user.Spec.Database)

		// Set owner reference
		return controllerutil.SetControllerReference(user, secret, r.Scheme)
	})

	return err
}

func (r *PostgresUserReconciler) updateStatus(ctx context.Context, user *databasev1alpha1.PostgresUser, ready bool, message string) error {
	user.Status.Ready = ready
	user.Status.Message = message

	// Update conditions
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconciliationFailed",
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}

	if ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ReconciliationSucceeded"
	}

	user.Status.Conditions = []metav1.Condition{condition}

	return r.Status().Update(ctx, user)
}

func (r *PostgresUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&databasev1alpha1.PostgresUser{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func generatePassword(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
