package provisioner

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Image versions follow Supabase's own self-hosting compose file, so the
// services are combinations Supabase tests together.
const (
	postgresImage = "supabase/postgres:17.6.1.136"
	authImage     = "supabase/gotrue:v2.197.0"
	restImage     = "postgrest/postgrest:v14.18"
	poolerImage   = "edoburu/pgbouncer:v1.25.1-p0"
)

const jwtExpiry = "3600"

// postgresContainers is a Postgres project: a one-shot setup (or restore),
// the database, a transaction pooler, the REST API (PostgREST) and the auth
// service (GoTrue).
func postgresContainers(s Spec) []container {
	db := "flobase-" + s.Ref + "-db:5432"
	maxConn := strconv.Itoa(max(100, s.Plan.MemoryMB/4))
	data := mount{volume: vol(s.Ref, "db-data"), path: "/var/lib/postgresql/data"}
	// The config volume keeps pgsodium's key and the archive settings.
	config := mount{volume: vol(s.Ref, "db-config"), path: "/etc/postgresql-custom"}
	backups := mount{volume: vol(s.Ref, "backups"), path: "/backups"}
	first := container{
		suffix: "setup", image: postgresImage, user: "root", oneShot: true,
		cmd:    []string{"sh", "-c", setupScript},
		mounts: []mount{config, backups},
	}
	if r := s.Restore; r != nil {
		target := ""
		switch {
		case r.Immediate:
			target = "immediate"
		case !r.Target.IsZero():
			target = r.Target.UTC().Format("2006-01-02 15:04:05.000000+00")
		}
		// The source's backups are mounted read-only: its own volumes, or
		// what the fetch step downloaded from off-site storage.
		sources := []mount{
			{volume: vol(r.FromRef, "backups"), path: "/restore", readOnly: true},
			{volume: vol(r.FromRef, "db-config"), path: "/restore-config", readOnly: true},
		}
		configDir := "/restore-config"
		if r.Offsite {
			sources = []mount{{volume: vol(s.Ref, "incoming"), path: "/restore", readOnly: true}}
			configDir = "/restore/config"
		}
		first = container{
			suffix: "restore", image: postgresImage, user: "root", oneShot: true,
			cmd: []string{"sh", "-c", restoreScript},
			env: []string{
				"BACKUP=" + safeName(r.Backup), "TARGET=" + target,
				"RESTORE_CONFIG=" + configDir, "NEW_PASSWORD=" + s.Password,
			},
			mounts: append([]mount{data, config, backups}, sources...),
		}
	}
	return []container{
		first,
		{
			suffix: "db",
			image:  postgresImage,
			cmd:    []string{"postgres", "-c", "config_file=/etc/postgresql/postgresql.conf", "-c", "log_min_messages=fatal"},
			env: []string{
				"POSTGRES_HOST=/var/run/postgresql", "PGPORT=5432", "POSTGRES_PORT=5432",
				"PGPASSWORD=" + s.Password, "POSTGRES_PASSWORD=" + s.Password,
				"PGDATABASE=postgres", "POSTGRES_DB=postgres", "JWT_EXP=" + jwtExpiry,
			},
			port:        5432,
			mounts:      []mount{data, config, backups},
			healthcheck: []string{"CMD", "pg_isready", "-U", "postgres", "-h", "localhost"},
			ready: func(ctx context.Context, host string, port int) error {
				return initRoles(ctx, host, port, s.Password)
			},
		},
		{
			suffix: "pooler",
			image:  poolerImage,
			env: []string{
				"DATABASE_URL=postgres://postgres:" + s.Password + "@" + db + "/postgres",
				"LISTEN_PORT=6432", "POOL_MODE=transaction", "AUTH_TYPE=scram-sha-256",
				"MAX_CLIENT_CONN=" + maxConn, "DEFAULT_POOL_SIZE=20",
			},
			port: 6432,
		},
		{
			suffix: "rest",
			image:  restImage,
			cmd:    []string{"postgrest"},
			env: []string{
				"PGRST_DB_URI=postgres://authenticator:" + s.Password + "@" + db + "/postgres",
				"PGRST_DB_SCHEMAS=public,graphql_public", "PGRST_DB_EXTRA_SEARCH_PATH=public",
				"PGRST_DB_MAX_ROWS=1000", "PGRST_DB_ANON_ROLE=anon",
				"PGRST_ADMIN_SERVER_PORT=3001", "PGRST_ADMIN_SERVER_HOST=localhost",
				"PGRST_JWT_SECRET=" + s.JWTSecret, "PGRST_DB_USE_LEGACY_GUCS=false",
				"PGRST_APP_SETTINGS_JWT_EXP=" + jwtExpiry,
			},
			healthcheck: []string{"CMD", "postgrest", "--ready"},
		},
		{
			suffix: "auth",
			image:  authImage,
			env: []string{
				"GOTRUE_API_HOST=0.0.0.0", "GOTRUE_API_PORT=9999", "API_EXTERNAL_URL=" + s.AuthURL,
				"GOTRUE_DB_DRIVER=postgres",
				"GOTRUE_DB_DATABASE_URL=postgres://supabase_auth_admin:" + s.Password + "@" + db + "/postgres",
				"GOTRUE_SITE_URL=http://localhost:3000", "GOTRUE_DISABLE_SIGNUP=false",
				"GOTRUE_JWT_ADMIN_ROLES=service_role", "GOTRUE_JWT_AUD=authenticated",
				"GOTRUE_JWT_DEFAULT_GROUP_NAME=authenticated", "GOTRUE_JWT_EXP=" + jwtExpiry,
				"GOTRUE_JWT_SECRET=" + s.JWTSecret, "GOTRUE_JWT_ISSUER=" + s.AuthURL,
				"GOTRUE_EXTERNAL_EMAIL_ENABLED=true", "GOTRUE_EXTERNAL_PHONE_ENABLED=false",
				// No SMTP server is configured yet, so email addresses are
				// confirmed on sign-up.
				"GOTRUE_MAILER_AUTOCONFIRM=true",
			},
			healthcheck: []string{"CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:9999/health"},
		},
	}
}

// setupScript prepares a new project's volumes before Postgres first starts:
// archiving of every change to the backups volume, and a pg_hba.conf that
// lets pg_basebackup in over the local socket. Docker fills the config
// volume from the image on first mount, so the script only adds files.
const setupScript = `set -e
mkdir -p /backups/wal /backups/base
chown -R postgres:postgres /backups
cat > /etc/postgresql-custom/conf.d/flobase-backups.conf <<'CONF'
archive_mode = on
archive_command = 'test ! -f /backups/wal/%f && cp %p /backups/wal/%f.part && mv /backups/wal/%f.part /backups/wal/%f'
archive_timeout = 60
hba_file = '/etc/postgresql-custom/pg_hba.conf'
CONF
{ echo "local replication supabase_admin trust"; cat /etc/postgresql/pg_hba.conf; } > /etc/postgresql-custom/pg_hba.conf
chown postgres:postgres /etc/postgresql-custom/conf.d/flobase-backups.conf /etc/postgresql-custom/pg_hba.conf`

// restoreScript builds a new project's data directory from another
// project's base backup, replays its archived changes to TARGET (empty =
// as far as they go), gives the superusers the new project's password over
// the local socket, and stops the server, ready for a normal start. The
// source is mounted read-only, so it is never touched, and its passwords
// are never needed.
const restoreScript = `set -e
PGDATA=/var/lib/postgresql/data
cp -a "$RESTORE_CONFIG/." /etc/postgresql-custom/
mkdir -p /backups/wal /backups/base "$PGDATA"
tar -xzf "/restore/base/$BACKUP/base.tar.gz" -C "$PGDATA"
if [ -f "/restore/base/$BACKUP/pg_wal.tar.gz" ]; then tar -xzf "/restore/base/$BACKUP/pg_wal.tar.gz" -C "$PGDATA/pg_wal"; fi
chown -R postgres:postgres "$PGDATA" /etc/postgresql-custom /backups
chmod 700 "$PGDATA"
touch "$PGDATA/recovery.signal"
{
  echo "restore_command = 'cp /restore/wal/%f %p'"
  echo "recovery_target_action = 'promote'"
  if [ "$TARGET" = immediate ]; then echo "recovery_target = 'immediate'"
  elif [ -n "$TARGET" ]; then echo "recovery_target_time = '$TARGET'"; fi
} >> "$PGDATA/postgresql.auto.conf"
if ! gosu postgres pg_ctl -D "$PGDATA" -w -t 1200 -l /tmp/restore.log start \
  -o "-c config_file=/etc/postgresql/postgresql.conf -c listen_addresses='' -c archive_mode=off"; then
  tail -n 15 /tmp/restore.log; exit 1
fi
i=0
until [ "$(gosu postgres psql -h /var/run/postgresql -U supabase_admin -d postgres -tAc 'select pg_is_in_recovery()')" = f ]; do
  i=$((i+1)); [ $i -lt 120 ] || { tail -n 15 /tmp/restore.log; exit 1; }; sleep 1
done
gosu postgres psql -h /var/run/postgresql -U supabase_admin -d postgres -q -v ON_ERROR_STOP=1 -v pw="$NEW_PASSWORD" <<'SQL'
alter user supabase_admin with password :'pw';
alter user postgres with password :'pw';
SQL
sed -i '/^restore_command/d;/^recovery_target/d' "$PGDATA/postgresql.auto.conf"
gosu postgres pg_ctl -D "$PGDATA" -w -m fast stop
echo "restored $BACKUP to ${TARGET:-latest}"`

// initRoles sets passwords on the roles the services log in as, which the
// Supabase image creates without one (or, after a restore, still carry the
// source's). It runs once the database is healthy.
func initRoles(ctx context.Context, host string, port int, password string) error {
	url := fmt.Sprintf("postgres://supabase_admin:%s@%s:%d/postgres?sslmode=disable", password, host, port)
	var conn *pgx.Conn
	var err error
	// The image restarts Postgres once after first-time setup, so the first
	// healthy moment can be followed by a short outage.
	for attempt := 0; attempt < 30; attempt++ {
		if conn, err = pgx.Connect(ctx, url); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil {
		return fmt.Errorf("connect to set up roles: %w", err)
	}
	defer conn.Close(ctx)
	quoted := "'" + strings.ReplaceAll(password, "'", "''") + "'"
	sql := ""
	// supabase_functions_admin joins this list when database webhooks ship.
	for _, role := range []string{"authenticator", "pgbouncer", "supabase_auth_admin", "supabase_storage_admin"} {
		sql += "alter user " + role + " with password " + quoted + ";\n"
	}
	sql += `alter database postgres set "app.settings.jwt_exp" to '` + jwtExpiry + "';\n"
	// GraphQL is on by default, as in Supabase; the image's event trigger
	// swaps graphql_public.graphql from a placeholder to the real resolver.
	sql += "create extension if not exists pg_graphql;"
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("set up roles: %w", err)
	}
	return nil
}
