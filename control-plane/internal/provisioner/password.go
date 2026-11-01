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

// pgRoles are the Postgres project roles that log in with the project's
// password: the two superusers and the roles initRoles sets for services.
var pgRoles = []string{"supabase_admin", "postgres", "authenticator", "pgbouncer", "supabase_auth_admin", "supabase_storage_admin"}

// SetPassword changes the password of every database user that has the
// project's password, logging in with the old one. Services keep using the
// old password until Recreate.
func (d *Docker) SetPassword(ctx context.Context, s Spec, old string) error {
	switch s.Engine {
	case "postgres":
		published, err := d.hostPort(ctx, "flobase-"+s.Ref+"-db", 5432)
		if err != nil {
			return err
		}
		host, port := d.Addr(s.Ref, "db", 5432, published)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://supabase_admin:%s@%s:%d/postgres?sslmode=disable", old, host, port))
		if err != nil {
			return fmt.Errorf("connect to the database: %w", err)
		}
		defer conn.Close(ctx)
		quoted := "'" + strings.ReplaceAll(s.Password, "'", "''") + "'"
		stmt := ""
		for _, role := range pgRoles {
			stmt += "alter user " + role + " with password " + quoted + ";\n"
		}
		// Several statements in one call run as one transaction.
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("change passwords: %w", err)
		}
		return nil
	case "mysql":
		published, err := d.hostPort(ctx, "flobase-"+s.Ref+"-db", 3306)
		if err != nil {
			return err
		}
		host, port := d.Addr(s.Ref, "db", 3306, published)
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd = "root", old
		cfg.Net, cfg.Addr = "tcp", host+":"+strconv.Itoa(port)
		cfg.MultiStatements = true
		db, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			return err
		}
		defer db.Close()
		quoted := "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s.Password) + "'"
		user := "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s.User) + "'@'%'"
		stmt := "alter user if exists 'root'@'%' identified by " + quoted + ", 'root'@'localhost' identified by " + quoted +
			", " + user + " identified by " + quoted
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("change passwords: %w", err)
		}
		return nil
	case "mongodb":
		// FerretDB checks MongoDB logins against the Postgres role behind
		// it. The local socket needs no password, and the new one reaches
		// psql through the environment, not the command line.
		script := `psql -q -v ON_ERROR_STOP=1 -U "$PGROLE" -d postgres -v role="$PGROLE" -v pw="$NEW_PASSWORD" <<'SQL'
alter user :"role" with password :'pw';
SQL`
		_, err := d.execEnv(ctx, "flobase-"+s.Ref+"-pg", "postgres",
			[]string{"PGROLE=" + s.User, "NEW_PASSWORD=" + s.Password}, "sh", "-c", script)
		if err != nil {
			return fmt.Errorf("change password: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unknown engine %q", s.Engine)
}

// Recreate replaces a project's running containers with ones built from s,
// in start order and on the same host ports, so connection strings keep
// working. The data volumes stay. Open connections end.
func (d *Docker) Recreate(ctx context.Context, s Spec) error {
	specs, _, err := containers(s)
	if err != nil {
		return err
	}
	network := "flobase-" + s.Ref
	for _, c := range specs {
		if c.oneShot {
			continue
		}
		name := "flobase-" + s.Ref + "-" + c.suffix
		if c.port != 0 {
			if c.hostPort, err = d.hostPort(ctx, name, c.port); err != nil {
				return err
			}
		}
		if err := d.call(ctx, "DELETE", "/containers/"+name+"?force=true", nil, nil); err != nil && !isNotFound(err) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
		if err := d.run(ctx, s, c, name, network); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		if c.healthcheck != nil {
			if err := d.waitHealthy(ctx, name); err != nil {
				return err
			}
		}
		if c.ready != nil {
			host, port := d.Addr(s.Ref, c.suffix, c.port, c.hostPort)
			if err := c.ready(ctx, host, port); err != nil {
				return err
			}
		}
	}
	return nil
}
