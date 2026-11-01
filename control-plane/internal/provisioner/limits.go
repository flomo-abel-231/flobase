package provisioner

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

// SetReadOnly turns writes off (or back on) for a project's database, as
// FloBase does when it outgrows its plan. Postgres and the Postgres behind
// FerretDB get default_transaction_read_only for every new session, and
// open sessions are ended so they reconnect read-only; MySQL marks the
// schema read only. A session can still turn it off for itself to delete
// data (FloBase's SQL editor does).
func (d *Docker) SetReadOnly(ctx context.Context, s Spec, on bool) error {
	switch s.Engine {
	case "postgres":
		published, err := d.hostPort(ctx, "flobase-"+s.Ref+"-db", 5432)
		if err != nil {
			return err
		}
		host, port := d.Addr(s.Ref, "db", 5432, published)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://supabase_admin:%s@%s:%d/postgres?sslmode=disable", s.Password, host, port))
		if err != nil {
			return fmt.Errorf("connect to the database: %w", err)
		}
		defer conn.Close(ctx)
		return pgReadOnly(ctx, conn, s.Database, "supabase_admin", on)
	case "mysql":
		published, err := d.hostPort(ctx, "flobase-"+s.Ref+"-db", 3306)
		if err != nil {
			return err
		}
		host, port := d.Addr(s.Ref, "db", 3306, published)
		db, err := mysqlRoot(host, port, s.Password)
		if err != nil {
			return err
		}
		defer db.Close()
		return MySQLReadOnly(ctx, db, s.Database, on)
	case "mongodb":
		value := "off"
		if on {
			value = "on"
		}
		// Each statement runs on its own, so the first can let this
		// session write even when the database is read-only already.
		script := fmt.Sprintf(`psql -q -v ON_ERROR_STOP=1 -U "$PGROLE" -d postgres <<'SQL'
set default_transaction_read_only = off;
alter database postgres set default_transaction_read_only = %s;
select pg_terminate_backend(pid) from pg_stat_activity
	where datname = 'postgres' and pid <> pg_backend_pid() and backend_type = 'client backend';
SQL`, value)
		if _, err := d.execEnv(ctx, "flobase-"+s.Ref+"-pg", "postgres", []string{"PGROLE=" + s.User}, "sh", "-c", script); err != nil {
			return fmt.Errorf("set read-only: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unknown engine %q", s.Engine)
}

func pgReadOnly(ctx context.Context, conn *pgx.Conn, database, self string, on bool) error {
	value := "off"
	if on {
		value = "on"
	}
	if _, err := conn.Exec(ctx, "set default_transaction_read_only = off"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "alter database "+pgx.Identifier{database}.Sanitize()+" set default_transaction_read_only = "+value); err != nil {
		return fmt.Errorf("set read-only: %w", err)
	}
	// Open sessions keep the setting they started with: end them, and the
	// apps and services reconnect with the new one.
	_, err := conn.Exec(ctx, `select pg_terminate_backend(pid) from pg_stat_activity
		where datname = $1 and pid <> pg_backend_pid() and backend_type = 'client backend' and usename <> $2`, database, self)
	return err
}

func mysqlRoot(host string, port int, password string) (*sql.DB, error) {
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd = "root", password
	cfg.Net, cfg.Addr = "tcp", host+":"+strconv.Itoa(port)
	return sql.Open("mysql", cfg.FormatDSN())
}

// MySQLReadOnly marks a schema read only, or writable again.
func MySQLReadOnly(ctx context.Context, db *sql.DB, schema string, on bool) error {
	value := "0"
	if on {
		value = "1"
	}
	quoted := "`" + strings.ReplaceAll(schema, "`", "``") + "`"
	if _, err := db.ExecContext(ctx, "alter schema "+quoted+" read only = "+value); err != nil {
		return fmt.Errorf("set read-only: %w", err)
	}
	return nil
}

// MongoSize is the size of the Postgres database behind a FerretDB project.
func (d *Docker) MongoSize(ctx context.Context, s Spec) (int64, error) {
	out, err := d.execEnv(ctx, "flobase-"+s.Ref+"-pg", "postgres", []string{"PGROLE=" + s.User},
		"sh", "-c", `psql -Atq -U "$PGROLE" -d postgres -c "select pg_database_size('postgres')"`)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}
