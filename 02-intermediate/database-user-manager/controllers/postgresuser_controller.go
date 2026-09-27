package controllers

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	logger := log.FromContext(ctx)

	// Fetch the PostgresUser
	user := &databasev1alpha1.PostgresUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	eng, err := engineFor(user.Spec.Engine)
	if err != nil {
		r.updateStatus(ctx, user, false, err.Error())
		// Invalid spec: no point retrying until the resource changes
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if !user.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, user, eng)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(user, finalizerName) {
		controllerutil.AddFinalizer(user, finalizerName)
		if err := r.Update(ctx, user); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve admin credentials
	adminUser, adminPassword, err := r.adminCredentials(ctx, user)
	if err != nil {
		logger.Error(err, "Failed to get admin credentials")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Admin credentials unavailable: %v", err))
		return ctrl.Result{}, err
	}

	// Connect to the admin database
	db, err := r.connect(ctx, eng, user, adminUser, adminPassword, eng.AdminDatabase())
	if err != nil {
		logger.Error(err, "Failed to connect to database")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Connection failed: %v", err))
		return ctrl.Result{}, err
	}
	defer db.Close()

	// Check if user exists
	exists, err := eng.UserExists(ctx, db, user.Spec.Username)
	if err != nil {
		logger.Error(err, "Failed to check if user exists")
		return ctrl.Result{}, err
	}

	// The stored password, if the credentials secret is still around
	storedPassword, secretFound := r.storedPassword(ctx, user)

	// Rotate when the user is missing, the secret is gone, the manual
	// toggle changed, or the rotation interval elapsed
	rotate := !exists || !secretFound ||
		user.Spec.RotatePassword != user.Status.ObservedRotatePassword ||
		rotationDue(user)

	var password string
	if rotate {
		password = generatePassword(32)

		stmt := eng.CreateUserSQL(user.Spec.Username, password)
		if exists {
			stmt = eng.SetPasswordSQL(user.Spec.Username, password)
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			logger.Error(err, "Failed to create/update user")
			r.updateStatus(ctx, user, false, fmt.Sprintf("User creation failed: %v", err))
			return ctrl.Result{}, err
		}

		now := metav1.Now()
		user.Status.LastPasswordRotation = &now
	} else {
		password = storedPassword
	}

	// Grant privileges on all requested databases
	if err := r.grantPrivileges(ctx, eng, user, adminUser, adminPassword, db); err != nil {
		logger.Error(err, "Failed to grant privileges")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Privilege grant failed: %v", err))
		return ctrl.Result{}, err
	}

	// Grant role memberships
	if err := r.grantRoles(ctx, eng, db, user); err != nil {
		logger.Error(err, "Failed to grant roles")
		r.updateStatus(ctx, user, false, fmt.Sprintf("Role grant failed: %v", err))
		return ctrl.Result{}, err
	}

	// Create or update secret with credentials
	if err := r.createOrUpdateSecret(ctx, eng, user, password); err != nil {
		logger.Error(err, "Failed to create/update secret")
		return ctrl.Result{}, err
	}

	// Only mark the toggle as observed once the new password is safely
	// stored, so a failed retry rotates again rather than desyncing
	if rotate {
		user.Status.ObservedRotatePassword = user.Spec.RotatePassword
	}

	// Update status
	r.updateStatus(ctx, user, true, "User ready")

	logger.Info("Successfully reconciled PostgresUser")
	return ctrl.Result{RequeueAfter: nextRotationIn(user)}, nil
}

func (r *PostgresUserReconciler) handleDeletion(ctx context.Context, user *databasev1alpha1.PostgresUser, eng dbEngine) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(user, finalizerName) {
		adminUser, adminPassword, err := r.adminCredentials(ctx, user)
		if err != nil {
			logger.Error(err, "Failed to get admin credentials for cleanup")
		} else if db, err := r.connect(ctx, eng, user, adminUser, adminPassword, eng.AdminDatabase()); err != nil {
			logger.Error(err, "Failed to connect to database for cleanup")
		} else {
			defer db.Close()

			// Drop user
			if _, err := db.ExecContext(ctx, eng.DropUserSQL(user.Spec.Username)); err != nil {
				logger.Error(err, "Failed to drop user")
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

func (r *PostgresUserReconciler) adminCredentials(ctx context.Context, user *databasev1alpha1.PostgresUser) (string, string, error) {
	namespace := user.Spec.AdminSecretRef.Namespace
	if namespace == "" {
		namespace = user.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      user.Spec.AdminSecretRef.Name,
		Namespace: namespace,
	}, secret); err != nil {
		return "", "", fmt.Errorf("failed to get admin secret: %w", err)
	}

	return string(secret.Data["username"]), string(secret.Data["password"]), nil
}

func (r *PostgresUserReconciler) connect(ctx context.Context, eng dbEngine, user *databasev1alpha1.PostgresUser, adminUser, adminPassword, database string) (*sql.DB, error) {
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

// storedPassword returns the password from the credentials secret and whether
// it was found, so a missing secret triggers password regeneration.
func (r *PostgresUserReconciler) storedPassword(ctx context.Context, user *databasev1alpha1.PostgresUser) (string, bool) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      user.Spec.SecretName,
		Namespace: user.Namespace,
	}, secret); err != nil {
		return "", false
	}
	password := string(secret.Data["password"])
	return password, password != ""
}

func (r *PostgresUserReconciler) grantPrivileges(ctx context.Context, eng dbEngine, user *databasev1alpha1.PostgresUser, adminUser, adminPassword string, adminDB *sql.DB) error {
	// Privileges are interpolated into GRANT statements, so validate them first
	for _, priv := range user.Spec.Privileges {
		if !eng.ValidPrivilege(priv) {
			return fmt.Errorf("invalid privilege %q for engine %s", priv, eng.Driver())
		}
	}

	for _, database := range allDatabases(user) {
		// Database-level grant on the admin connection (postgres only)
		if stmt := eng.DatabaseGrantSQL(database, user.Spec.Username); stmt != "" {
			if _, err := adminDB.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}

		// Table-level grants must run while connected to the target database
		targetDB, err := r.connect(ctx, eng, user, adminUser, adminPassword, database)
		if err != nil {
			return err
		}
		for _, priv := range user.Spec.Privileges {
			for _, stmt := range eng.PrivilegeGrantSQL(priv, database, user.Spec.Username) {
				if _, err := targetDB.ExecContext(ctx, stmt); err != nil {
					targetDB.Close()
					return err
				}
			}
		}
		if err := targetDB.Close(); err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresUserReconciler) grantRoles(ctx context.Context, eng dbEngine, db *sql.DB, user *databasev1alpha1.PostgresUser) error {
	for _, role := range user.Spec.Roles {
		if _, err := db.ExecContext(ctx, eng.RoleGrantSQL(role, user.Spec.Username)); err != nil {
			return fmt.Errorf("granting role %q: %w", role, err)
		}
	}
	return nil
}

func (r *PostgresUserReconciler) createOrUpdateSecret(ctx context.Context, eng dbEngine, user *databasev1alpha1.PostgresUser, password string) error {
	port := user.Spec.Port
	if port == 0 {
		port = eng.DefaultPort()
	}

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
		secret.Data["port"] = fmt.Appendf(nil, "%d", port)
		secret.Data["database"] = []byte(user.Spec.Database)
		secret.Data["databases"] = []byte(strings.Join(allDatabases(user), ","))
		secret.Data["engine"] = []byte(eng.Driver())

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
		ObservedGeneration: user.Generation,
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

// allDatabases returns the primary database plus any additional databases.
func allDatabases(user *databasev1alpha1.PostgresUser) []string {
	return append([]string{user.Spec.Database}, user.Spec.Databases...)
}

// rotationDue reports whether the automatic rotation interval has elapsed.
func rotationDue(user *databasev1alpha1.PostgresUser) bool {
	if user.Spec.RotationInterval == nil {
		return false
	}
	if user.Status.LastPasswordRotation == nil {
		return true
	}
	return time.Since(user.Status.LastPasswordRotation.Time) >= user.Spec.RotationInterval.Duration
}

// nextRotationIn returns how long until the next automatic rotation is due.
func nextRotationIn(user *databasev1alpha1.PostgresUser) time.Duration {
	if user.Spec.RotationInterval == nil {
		return 0
	}
	if user.Status.LastPasswordRotation == nil {
		return user.Spec.RotationInterval.Duration
	}
	next := user.Status.LastPasswordRotation.Add(user.Spec.RotationInterval.Duration)
	if d := time.Until(next); d > 0 {
		return d
	}
	return time.Minute
}

func generatePassword(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return base64.URLEncoding.EncodeToString(bytes)[:length]
}
