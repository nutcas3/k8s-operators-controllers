package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// dbEngine abstracts the SQL dialect differences between the supported
// database engines so the reconciler stays engine-agnostic.
type dbEngine interface {
	// Driver is the database/sql driver name.
	Driver() string
	// DefaultPort is used when spec.port is unset.
	DefaultPort() int32
	// AdminDatabase is the database the admin connection uses.
	AdminDatabase() string
	// DSN builds a driver-specific connection string.
	DSN(host string, port int32, user, password, database string) string
	// UserExists reports whether username already exists.
	UserExists(ctx context.Context, db *sql.DB, username string) (bool, error)
	// CreateUserSQL creates the user with the given password.
	CreateUserSQL(username, password string) string
	// SetPasswordSQL rotates the password of an existing user.
	SetPasswordSQL(username, password string) string
	// DropUserSQL removes the user.
	DropUserSQL(username string) string
	// DatabaseGrantSQL returns a database-level grant run on the admin
	// connection, or "" when the engine doesn't need one.
	DatabaseGrantSQL(database, username string) string
	// PrivilegeGrantSQL returns the statements granting privilege inside
	// database to username, run while connected to database.
	PrivilegeGrantSQL(privilege, database, username string) []string
	// RoleGrantSQL grants an existing role to the user.
	RoleGrantSQL(role, username string) string
	// ValidPrivilege reports whether privilege is a known grantable keyword.
	// Privileges can't be parameterized in GRANT statements, so they must
	// be validated before being interpolated into SQL.
	ValidPrivilege(privilege string) bool
}

func engineFor(name string) (dbEngine, error) {
	switch name {
	case "", "postgres":
		return postgresEngine{}, nil
	case "mysql":
		return mysqlEngine{}, nil
	default:
		return nil, fmt.Errorf("unsupported engine %q", name)
	}
}

// postgresEngine implements dbEngine for PostgreSQL.
type postgresEngine struct{}

func (postgresEngine) Driver() string        { return "postgres" }
func (postgresEngine) DefaultPort() int32    { return 5432 }
func (postgresEngine) AdminDatabase() string { return "postgres" }

func (postgresEngine) DSN(host string, port int32, user, password, database string) string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, database)
}

func (postgresEngine) UserExists(ctx context.Context, db *sql.DB, username string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", username).Scan(&exists)
	return exists, err
}

func (postgresEngine) CreateUserSQL(username, password string) string {
	return fmt.Sprintf("CREATE USER %s WITH PASSWORD %s",
		pq.QuoteIdentifier(username), pq.QuoteLiteral(password))
}

func (postgresEngine) SetPasswordSQL(username, password string) string {
	return fmt.Sprintf("ALTER USER %s WITH PASSWORD %s",
		pq.QuoteIdentifier(username), pq.QuoteLiteral(password))
}

func (postgresEngine) DropUserSQL(username string) string {
	return fmt.Sprintf("DROP USER IF EXISTS %s", pq.QuoteIdentifier(username))
}

func (postgresEngine) DatabaseGrantSQL(database, username string) string {
	return fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s",
		pq.QuoteIdentifier(database), pq.QuoteIdentifier(username))
}

func (postgresEngine) PrivilegeGrantSQL(privilege, database, username string) []string {
	priv := strings.ToUpper(privilege)
	return []string{
		fmt.Sprintf("GRANT %s ON ALL TABLES IN SCHEMA public TO %s",
			priv, pq.QuoteIdentifier(username)),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT %s ON TABLES TO %s",
			priv, pq.QuoteIdentifier(username)),
	}
}

func (postgresEngine) RoleGrantSQL(role, username string) string {
	return fmt.Sprintf("GRANT %s TO %s",
		pq.QuoteIdentifier(role), pq.QuoteIdentifier(username))
}

func (postgresEngine) ValidPrivilege(privilege string) bool {
	return postgresPrivileges[strings.ToUpper(privilege)]
}

var postgresPrivileges = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"TRUNCATE": true, "REFERENCES": true, "TRIGGER": true, "EXECUTE": true,
	"USAGE": true, "CREATE": true, "CONNECT": true, "TEMPORARY": true,
	"TEMP": true, "ALL": true,
}

// mysqlEngine implements dbEngine for MySQL.
// MySQL accounts are 'user'@'host'; this engine manages 'user'@'%'.
type mysqlEngine struct{}

func (mysqlEngine) Driver() string        { return "mysql" }
func (mysqlEngine) DefaultPort() int32    { return 3306 }
func (mysqlEngine) AdminDatabase() string { return "mysql" }

func (mysqlEngine) DSN(host string, port int32, user, password, database string) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, database)
}

func (mysqlEngine) UserExists(ctx context.Context, db *sql.DB, username string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM mysql.user WHERE User = ? AND Host = '%')", username).Scan(&exists)
	return exists, err
}

func (mysqlEngine) CreateUserSQL(username, password string) string {
	return fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s",
		mysqlAccount(username), mysqlLiteral(password))
}

func (mysqlEngine) SetPasswordSQL(username, password string) string {
	return fmt.Sprintf("ALTER USER %s IDENTIFIED BY %s",
		mysqlAccount(username), mysqlLiteral(password))
}

func (mysqlEngine) DropUserSQL(username string) string {
	return fmt.Sprintf("DROP USER IF EXISTS %s", mysqlAccount(username))
}

func (mysqlEngine) DatabaseGrantSQL(database, username string) string { return "" }

func (mysqlEngine) PrivilegeGrantSQL(privilege, database, username string) []string {
	return []string{
		fmt.Sprintf("GRANT %s ON %s.* TO %s",
			strings.ToUpper(privilege), mysqlIdent(database), mysqlAccount(username)),
	}
}

func (mysqlEngine) RoleGrantSQL(role, username string) string {
	return fmt.Sprintf("GRANT %s TO %s", mysqlAccount(role), mysqlAccount(username))
}

func (mysqlEngine) ValidPrivilege(privilege string) bool {
	return mysqlPrivileges[strings.ToUpper(privilege)]
}

var mysqlPrivileges = map[string]bool{
	"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
	"CREATE": true, "DROP": true, "INDEX": true, "ALTER": true,
	"REFERENCES": true, "EXECUTE": true, "TRIGGER": true,
	"CREATE VIEW": true, "SHOW VIEW": true, "LOCK TABLES": true,
	"CREATE ROUTINE": true, "ALTER ROUTINE": true, "EVENT": true, "ALL": true,
}

var mysqlEscaper = strings.NewReplacer(`\`, `\\`, `'`, `\'`)

// mysqlAccount renders a MySQL account name 'user'@'%'.
func mysqlAccount(username string) string {
	return fmt.Sprintf("'%s'@'%%'", mysqlEscaper.Replace(username))
}

func mysqlLiteral(s string) string {
	return "'" + mysqlEscaper.Replace(s) + "'"
}

func mysqlIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
