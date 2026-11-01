package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"flobase/control-plane/internal/store"
)

// Data endpoints power the dashboard's SQL editor, table editor, auth users
// and logs. They connect to the project's database as its owner, so they sit
// behind the admin token like every other control-plane route.

const (
	queryTimeout = 30 * time.Second
	maxRows      = 1000
)

func (s *Server) dataRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v1/projects/{ref}/sql", s.auth(s.runSQL))
	mux.Handle("GET /api/v1/projects/{ref}/schemas", s.auth(s.listSchemas))
	mux.Handle("GET /api/v1/projects/{ref}/tables", s.auth(s.listTables))
	mux.Handle("POST /api/v1/projects/{ref}/tables", s.auth(s.createTable))
	mux.Handle("GET /api/v1/projects/{ref}/tables/{table}/rows", s.auth(s.tableRows))
	mux.Handle("POST /api/v1/projects/{ref}/tables/{table}/rows", s.auth(s.insertRow))
	mux.Handle("PATCH /api/v1/projects/{ref}/tables/{table}/rows", s.auth(s.updateRow))
	mux.Handle("DELETE /api/v1/projects/{ref}/tables/{table}/rows", s.auth(s.deleteRow))
	mux.Handle("GET /api/v1/projects/{ref}/users", s.auth(s.listUsers))
	mux.Handle("POST /api/v1/projects/{ref}/users", s.auth(s.createUser))
	mux.Handle("DELETE /api/v1/projects/{ref}/users/{id}", s.auth(s.deleteUser))
	mux.Handle("GET /api/v1/projects/{ref}/logs", s.auth(s.logs))
}

// activeProject loads a project and checks it is running, answering the
// request itself when it is not.
func (s *Server) activeProject(w http.ResponseWriter, r *http.Request, engines ...string) (store.Project, bool) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Project not found.")
		return p, false
	} else if err != nil {
		s.internal(w, err)
		return p, false
	}
	if p.Status != store.StatusActive {
		writeError(w, http.StatusConflict, "The project is not active.")
		return p, false
	}
	if len(engines) > 0 && !contains(engines, p.Engine) {
		writeError(w, http.StatusBadRequest, "Not available for "+p.Engine+" projects yet.")
		return p, false
	}
	return p, true
}

func (s *Server) pgConnect(ctx context.Context, p store.Project) (*pgx.Conn, error) {
	host, port := s.prov.Addr(p.Ref, "db", 5432, p.DBPort)
	cfg, err := pgx.ParseConfig(fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable", p.DBUser, host, port, p.DBName))
	if err != nil {
		return nil, err
	}
	cfg.Password = p.DBPassword
	cfg.RuntimeParams["statement_timeout"] = strconv.Itoa(int(queryTimeout.Milliseconds()))
	cfg.RuntimeParams["application_name"] = "flobase-dashboard"
	return pgx.ConnectConfig(ctx, cfg)
}

func (s *Server) mysqlOpen(p store.Project) (*sql.DB, error) {
	host, port := s.prov.Addr(p.Ref, "db", 3306, p.DBPort)
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = p.DBUser, p.DBPassword, p.DBName
	cfg.Net, cfg.Addr = "tcp", host+":"+strconv.Itoa(port)
	cfg.MultiStatements = true
	cfg.Timeout, cfg.ReadTimeout = 5*time.Second, queryTimeout
	return sql.Open("mysql", cfg.FormatDSN())
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Body must be valid JSON.")
		return false
	}
	return true
}

// dbError answers with the database's own message: it is the user's query
// that failed, not the platform.
func dbError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	var myErr *mysql.MySQLError
	switch {
	case errors.As(err, &pgErr):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": pgErr.Message, "detail": pgErr.Detail, "hint": pgErr.Hint, "position": pgErr.Position, "code": pgErr.Code,
		})
	case errors.As(err, &myErr):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": myErr.Message, "code": strconv.Itoa(int(myErr.Number))})
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "The query ran longer than 30 seconds and was stopped.")
	default:
		writeError(w, http.StatusBadGateway, "Could not reach the database: "+err.Error())
	}
}

type queryResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Command   string   `json:"command"`
	Truncated bool     `json:"truncated,omitempty"`
}

func (s *Server) runSQL(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres", "mysql")
	if !ok {
		return
	}
	var req struct {
		Query string `json:"query"`
		// ReadOnly runs the query in read-only transactions, so the
		// database refuses any change.
		ReadOnly bool `json:"read_only"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "Write a query first.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout+5*time.Second)
	defer cancel()
	start := time.Now()
	var results []queryResult
	var err error
	if p.Engine == "postgres" {
		results, err = s.runPostgres(ctx, p, req.Query, req.ReadOnly)
	} else {
		results, err = s.runMySQL(ctx, p, req.Query, req.ReadOnly)
	}
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "duration_ms": time.Since(start).Milliseconds()})
}

// runPostgres uses the simple query protocol, so a script of several
// statements runs as one batch, as it would in psql.
func (s *Server) runPostgres(ctx context.Context, p store.Project, query string, readOnly bool) ([]queryResult, error) {
	conn, err := s.pgConnect(ctx, p)
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())
	if readOnly {
		if _, err := conn.Exec(ctx, "set session characteristics as transaction read only"); err != nil {
			return nil, err
		}
	}
	all, err := conn.PgConn().Exec(ctx, query).ReadAll()
	if err != nil {
		return nil, err
	}
	results := make([]queryResult, 0, len(all))
	for _, res := range all {
		qr := queryResult{Columns: []string{}, Rows: [][]any{}, Command: res.CommandTag.String()}
		for _, f := range res.FieldDescriptions {
			qr.Columns = append(qr.Columns, f.Name)
		}
		for i, row := range res.Rows {
			if i == maxRows {
				qr.Truncated = true
				break
			}
			qr.Rows = append(qr.Rows, textRow(row))
		}
		results = append(results, qr)
	}
	return results, nil
}

func textRow(row [][]byte) []any {
	out := make([]any, len(row))
	for i, v := range row {
		if v != nil {
			out[i] = string(v)
		}
	}
	return out
}

func (s *Server) runMySQL(ctx context.Context, p store.Project, query string, readOnly bool) ([]queryResult, error) {
	db, err := s.mysqlOpen(p)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if readOnly {
		if _, err := conn.ExecContext(ctx, "SET SESSION TRANSACTION READ ONLY"); err != nil {
			return nil, err
		}
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var results []queryResult
	for {
		cols, err := rows.Columns()
		if err != nil {
			return nil, err
		}
		qr := queryResult{Columns: cols, Rows: [][]any{}, Command: "OK"}
		for rows.Next() {
			if len(qr.Rows) == maxRows {
				qr.Truncated = true
				continue
			}
			raw := make([]sql.RawBytes, len(cols))
			dest := make([]any, len(cols))
			for i := range raw {
				dest[i] = &raw[i]
			}
			if err := rows.Scan(dest...); err != nil {
				return nil, err
			}
			row := make([][]byte, len(raw))
			for i, v := range raw {
				if v != nil {
					row[i] = bytes.Clone(v)
				}
			}
			qr.Rows = append(qr.Rows, textRow(row))
		}
		if len(cols) > 0 {
			qr.Command = fmt.Sprintf("SELECT %d", len(qr.Rows))
		}
		results = append(results, qr)
		if !rows.NextResultSet() {
			break
		}
	}
	return results, rows.Err()
}

// Table editor: public schema of Postgres projects.

type tableInfo struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // table or view
	RLS      bool      `json:"rls"`
	Estimate *int64    `json:"row_estimate"` // nil until Postgres has counted the table
	Columns  []colInfo `json:"columns,omitempty"`
}

// The table editor works in any schema, as Supabase's does. Schemas the
// Supabase services own are shown read-only there; their changes belong
// to those services or the SQL editor.
var protectedSchemas = map[string]bool{
	"auth": true, "storage": true, "extensions": true, "graphql": true, "graphql_public": true,
	"pgbouncer": true, "realtime": true, "_realtime": true, "supabase_functions": true,
	"supabase_migrations": true, "vault": true, "net": true, "pgsodium": true, "pgsodium_masks": true,
	"cron": true, "_analytics": true,
}

// userSchema is the schema the request names (default public). It must
// exist and not be one of Postgres's own; every identifier built from it
// comes from the catalog, never the request.
func userSchema(ctx context.Context, conn *pgx.Conn, r *http.Request) (string, error) {
	name := r.URL.Query().Get("schema")
	if name == "" {
		name = "public"
	}
	var ok bool
	err := conn.QueryRow(ctx, `select exists (select 1 from pg_namespace where nspname = $1
		and nspname not like 'pg\_%' and nspname <> 'information_schema')`, name).Scan(&ok)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", store.ErrNotFound
	}
	return name, nil
}

type schemaInfo struct {
	Name      string `json:"name"`
	Tables    int    `json:"tables"`
	Protected bool   `json:"protected"`
}

func (s *Server) listSchemas(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	conn, err := s.pgConnect(r.Context(), p)
	if err != nil {
		dbError(w, err)
		return
	}
	defer conn.Close(context.Background())
	rows, err := conn.Query(r.Context(), `
		select n.nspname, count(c.oid) filter (where c.relkind in ('r', 'p', 'v', 'm'))
		from pg_namespace n left join pg_class c on c.relnamespace = n.oid
		where n.nspname not like 'pg\_%' and n.nspname <> 'information_schema'
		group by n.nspname order by n.nspname`)
	if err != nil {
		dbError(w, err)
		return
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (schemaInfo, error) {
		var sc schemaInfo
		err := row.Scan(&sc.Name, &sc.Tables)
		sc.Protected = protectedSchemas[sc.Name]
		return sc, err
	})
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type colInfo struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Nullable bool    `json:"nullable"`
	Default  *string `json:"default"`
	Primary  bool    `json:"primary"`
}

func (s *Server) listTables(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	conn, err := s.pgConnect(r.Context(), p)
	if err != nil {
		dbError(w, err)
		return
	}
	defer conn.Close(context.Background())
	schema, err := userSchema(r.Context(), conn, r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Schema not found.")
		return
	} else if err != nil {
		dbError(w, err)
		return
	}
	rows, err := conn.Query(r.Context(), `
		select c.relname, case when c.relkind in ('v', 'm') then 'view' else 'table' end,
			c.relrowsecurity, case when c.reltuples < 0 then null else c.reltuples::bigint end
		from pg_class c join pg_namespace n on n.oid = c.relnamespace
		where n.nspname = $1 and c.relkind in ('r', 'p', 'v', 'm')
		order by c.relname`, schema)
	if err != nil {
		dbError(w, err)
		return
	}
	tables, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (tableInfo, error) {
		var t tableInfo
		err := row.Scan(&t.Name, &t.Kind, &t.RLS, &t.Estimate)
		return t, err
	})
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tables)
}

// columns reads a table's or view's columns; an unknown one is
// ErrNotFound. Every identifier the editor puts in SQL comes from here,
// never the request.
func columns(ctx context.Context, conn *pgx.Conn, schema, table string) ([]colInfo, error) {
	rows, err := conn.Query(ctx, `
		select a.attname, format_type(a.atttypid, a.atttypmod), not a.attnotnull,
			pg_get_expr(d.adbin, d.adrelid),
			coalesce(a.attnum = any(i.indkey), false)
		from pg_attribute a
		join pg_class c on c.oid = a.attrelid
		join pg_namespace n on n.oid = c.relnamespace
		left join pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
		left join pg_index i on i.indrelid = c.oid and i.indisprimary
		where n.nspname = $1 and c.relname = $2 and c.relkind in ('r', 'p', 'v', 'm')
			and a.attnum > 0 and not a.attisdropped
		order by a.attnum`, schema, table)
	if err != nil {
		return nil, err
	}
	cols, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (colInfo, error) {
		var c colInfo
		err := row.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default, &c.Primary)
		return c, err
	})
	if err == nil && len(cols) == 0 {
		return nil, store.ErrNotFound
	}
	return cols, err
}

// relation is the table or view a table-editor request works on.
type relation struct {
	ident     string // schema-qualified and quoted
	view      bool
	protected bool
}

// withTable connects and loads the table named in the path, in the schema
// named in the query. Writes are refused in protected schemas and views.
func (s *Server) withTable(w http.ResponseWriter, r *http.Request, write bool, fn func(conn *pgx.Conn, rel relation, cols []colInfo)) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	conn, err := s.pgConnect(r.Context(), p)
	if err != nil {
		dbError(w, err)
		return
	}
	defer conn.Close(context.Background())
	schema, err := userSchema(r.Context(), conn, r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Schema not found.")
		return
	} else if err != nil {
		dbError(w, err)
		return
	}
	table := r.PathValue("table")
	cols, err := columns(r.Context(), conn, schema, table)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Table not found.")
		return
	} else if err != nil {
		dbError(w, err)
		return
	}
	rel := relation{ident: pgx.Identifier{schema, table}.Sanitize(), protected: protectedSchemas[schema]}
	var kind string
	if err := conn.QueryRow(r.Context(), "select relkind::text from pg_class where oid = $1::regclass", rel.ident).Scan(&kind); err != nil {
		dbError(w, err)
		return
	}
	rel.view = kind == "v" || kind == "m"
	if write && (rel.protected || rel.view) {
		writeError(w, http.StatusForbidden, "This is read-only in the table editor: "+
			map[bool]string{true: "views have no rows of their own.", false: "the " + schema + " schema belongs to FloBase's services."}[rel.view]+
			" Use the SQL editor to change it.")
		return
	}
	fn(conn, rel, cols)
}

func quoteCols(cols []colInfo, keep func(colInfo) bool) []string {
	var out []string
	for _, c := range cols {
		if keep(c) {
			out = append(out, pgx.Identifier{c.Name}.Sanitize())
		}
	}
	return out
}

func (s *Server) tableRows(w http.ResponseWriter, r *http.Request) {
	s.withTable(w, r, false, func(conn *pgx.Conn, rel relation, cols []colInfo) {
		ident := rel.ident
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 100
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		order := quoteCols(cols, func(c colInfo) bool { return c.Primary })
		if order == nil {
			// Views have no ctid; their first column gives a stable order.
			order = []string{"ctid"}
			if rel.view {
				order = []string{"1"}
			}
		}
		// Sorting by a column the person clicked; its name must be one of
		// the table's, so it comes from the catalog.
		if sort := r.URL.Query().Get("sort"); sort != "" {
			for _, c := range cols {
				if c.Name == sort {
					dir := "asc nulls last"
					if r.URL.Query().Get("dir") == "desc" {
						dir = "desc nulls last"
					}
					order = append([]string{pgx.Identifier{c.Name}.Sanitize() + " " + dir}, order...)
				}
			}
		}
		// Search matches the text of any column, like DataTables' search box.
		where, args := "", []any{limit, max(offset, 0)}
		if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
			like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
			where = " where t0::text ilike $3"
			args = append(args, like)
		}
		var total, filtered int64
		if err := conn.QueryRow(r.Context(), "select count(*) from "+ident).Scan(&total); err != nil {
			dbError(w, err)
			return
		}
		filtered = total
		if where != "" {
			if err := conn.QueryRow(r.Context(), "select count(*) from "+ident+" t0"+strings.Replace(where, "$3", "$1", 1), args[2]).Scan(&filtered); err != nil {
				dbError(w, err)
				return
			}
		}
		// to_jsonb gives every Postgres type a JSON form the browser can show.
		rows, err := conn.Query(r.Context(), "select to_jsonb(t) from (select t0.* from "+ident+" t0"+where+
			" order by "+strings.Join(order, ", ")+" limit $1 offset $2) t", args...)
		if err != nil {
			dbError(w, err)
			return
		}
		data, err := pgx.CollectRows(rows, pgx.RowTo[map[string]any])
		if err != nil {
			dbError(w, err)
			return
		}
		var rls bool
		_ = conn.QueryRow(r.Context(), "select relrowsecurity from pg_class where oid = $1::regclass", ident).Scan(&rls)
		writeJSON(w, http.StatusOK, map[string]any{
			"columns": cols, "rows": data, "total": total, "filtered": filtered, "rls": rls,
			"view": rel.view, "read_only": rel.view || rel.protected,
		})
	})
}

// Rows are written through jsonb_populate_record, so Postgres converts each
// JSON value to its column's type.

func pick(cols []colInfo, values map[string]any, primaryOnly bool) []colInfo {
	var out []colInfo
	for _, c := range cols {
		if _, ok := values[c.Name]; ok && (!primaryOnly || c.Primary) {
			out = append(out, c)
		}
	}
	return out
}

func names(cols []colInfo) string {
	return strings.Join(quoteCols(cols, func(colInfo) bool { return true }), ", ")
}

func (s *Server) insertRow(w http.ResponseWriter, r *http.Request) {
	s.withTable(w, r, true, func(conn *pgx.Conn, rel relation, cols []colInfo) {
		ident := rel.ident
		var req struct {
			Values map[string]any `json:"values"`
		}
		if !decode(w, r, &req) {
			return
		}
		set := pick(cols, req.Values, false)
		var err error
		if len(set) == 0 {
			_, err = conn.Exec(r.Context(), "insert into "+ident+" default values")
		} else {
			_, err = conn.Exec(r.Context(), "insert into "+ident+" ("+names(set)+") select "+names(set)+
				" from jsonb_populate_record(null::"+ident+", $1::jsonb)", req.Values)
		}
		if err != nil {
			dbError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
}

type rowChange struct {
	Match  map[string]any `json:"match"`
	Values map[string]any `json:"values"`
}

// matchClause selects one row by its primary key; statements alias the
// table as t so its columns never clash with the incoming values.
func matchClause(w http.ResponseWriter, ident string, cols []colInfo, match map[string]any, arg string) (string, bool) {
	pk := quoteCols(cols, func(c colInfo) bool { return c.Primary })
	if pk == nil {
		writeError(w, http.StatusBadRequest, "This table has no primary key, so rows can only be changed with SQL.")
		return "", false
	}
	if len(pick(cols, match, true)) != len(pk) {
		writeError(w, http.StatusBadRequest, "Give a value for every primary key column.")
		return "", false
	}
	target := make([]string, len(pk))
	for i, c := range pk {
		target[i] = "t." + c
	}
	return "(" + strings.Join(target, ", ") + ") = (select " + strings.Join(pk, ", ") +
		" from jsonb_populate_record(null::" + ident + ", " + arg + "::jsonb))", true
}

func (s *Server) updateRow(w http.ResponseWriter, r *http.Request) {
	s.withTable(w, r, true, func(conn *pgx.Conn, rel relation, cols []colInfo) {
		ident := rel.ident
		var req rowChange
		if !decode(w, r, &req) {
			return
		}
		where, ok := matchClause(w, ident, cols, req.Match, "$2")
		if !ok {
			return
		}
		set := pick(cols, req.Values, false)
		if len(set) == 0 {
			writeError(w, http.StatusBadRequest, "Nothing to change.")
			return
		}
		fromV := quoteCols(set, func(colInfo) bool { return true })
		for i, c := range fromV {
			fromV[i] = "v." + c
		}
		// ROW() keeps a one-column assignment valid.
		_, err := conn.Exec(r.Context(), "update "+ident+" as t set ("+names(set)+") = row("+strings.Join(fromV, ", ")+
			") from jsonb_populate_record(null::"+ident+", $1::jsonb) as v where "+where, req.Values, req.Match)
		if err != nil {
			dbError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) deleteRow(w http.ResponseWriter, r *http.Request) {
	s.withTable(w, r, true, func(conn *pgx.Conn, rel relation, cols []colInfo) {
		ident := rel.ident
		var req rowChange
		if !decode(w, r, &req) {
			return
		}
		where, ok := matchClause(w, ident, cols, req.Match, "$1")
		if !ok {
			return
		}
		if _, err := conn.Exec(r.Context(), "delete from "+ident+" as t where "+where, req.Match); err != nil {
			dbError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Column types the table editor offers; anything else is a SQL-editor job.
var columnTypes = map[string]string{
	"text": "text", "varchar": "varchar", "int4": "integer", "int8": "bigint", "float8": "double precision",
	"numeric": "numeric", "bool": "boolean", "uuid": "uuid", "timestamptz": "timestamptz", "date": "date", "jsonb": "jsonb",
}

func (s *Server) createTable(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	var req struct {
		Name    string `json:"name"`
		RLS     bool   `json:"rls"`
		Columns []struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Nullable bool   `json:"nullable"`
		} `json:"columns"`
	}
	if !decode(w, r, &req) {
		return
	}
	if !validIdent(req.Name) {
		writeError(w, http.StatusBadRequest, "Table names use lowercase letters, digits and underscores, starting with a letter.")
		return
	}
	// Every table gets an identity primary key and a creation time, as in
	// Supabase's table editor.
	defs := []string{"id bigint generated by default as identity primary key", "created_at timestamptz not null default now()"}
	for _, c := range req.Columns {
		typ, ok := columnTypes[c.Type]
		if !ok || !validIdent(c.Name) || c.Name == "id" || c.Name == "created_at" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Column %q needs a valid name and type.", c.Name))
			return
		}
		def := pgx.Identifier{c.Name}.Sanitize() + " " + typ
		if !c.Nullable {
			def += " not null"
		}
		defs = append(defs, def)
	}
	conn, err := s.pgConnect(r.Context(), p)
	if err != nil {
		dbError(w, err)
		return
	}
	defer conn.Close(context.Background())
	schema, err := userSchema(r.Context(), conn, r)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Schema not found.")
		return
	} else if err != nil {
		dbError(w, err)
		return
	}
	if protectedSchemas[schema] {
		writeError(w, http.StatusForbidden, "The "+schema+" schema belongs to FloBase's services; create tables in another schema.")
		return
	}
	ident := pgx.Identifier{schema, req.Name}.Sanitize()
	stmt := "create table " + ident + " (\n  " + strings.Join(defs, ",\n  ") + "\n);"
	if req.RLS {
		stmt += "\nalter table " + ident + " enable row level security;"
	}
	if _, err := conn.Exec(r.Context(), stmt); err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"sql": stmt})
}

func validIdent(s string) bool {
	if s == "" || len(s) > 63 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// Auth users: listed from auth.users, created and deleted through the auth
// service's admin API so passwords are hashed the way it expects.

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	conn, err := s.pgConnect(r.Context(), p)
	if err != nil {
		dbError(w, err)
		return
	}
	defer conn.Close(context.Background())
	rows, err := conn.Query(r.Context(), `
		select coalesce(jsonb_agg(jsonb_build_object('id', id, 'email', email, 'created_at', created_at,
			'last_sign_in_at', last_sign_in_at, 'confirmed_at', email_confirmed_at) order by created_at desc), '[]')
		from auth.users`)
	if err != nil {
		dbError(w, err)
		return
	}
	users, err := pgx.CollectExactlyOneRow(rows, pgx.RowTo[[]any])
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) authAdmin(ctx context.Context, p store.Project, method, path string, body any) (*http.Response, error) {
	host, port := s.prov.Addr(p.Ref, "auth", 9999, 0)
	if port == 0 {
		return nil, errors.New("the auth service is reachable only when FloBase runs with Docker Compose")
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://%s:%d/admin%s", host, port, path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.ServiceKey)
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

// relay passes the auth service's answer through, keeping its message.
func relay(w http.ResponseWriter, resp *http.Response, err error) {
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		var msg struct {
			Msg     string `json:"msg"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &msg)
		text := msg.Msg + msg.Message
		if text == "" {
			text = "The auth service refused the request."
		}
		writeError(w, resp.StatusCode, text)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) {
		return
	}
	resp, err := s.authAdmin(r.Context(), p, "POST", "/users", map[string]any{
		"email": req.Email, "password": req.Password, "email_confirm": true,
	})
	relay(w, resp, err)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.activeProject(w, r, "postgres")
	if !ok {
		return
	}
	resp, err := s.authAdmin(r.Context(), p, "DELETE", "/users/"+r.PathValue("id"), nil)
	relay(w, resp, err)
}

var logServices = map[string][]string{
	"postgres": {"db", "pooler", "rest", "auth", "offsite"},
	"mysql":    {"db"},
	"mongodb":  {"db", "pg"},
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.Project(r.Context(), r.PathValue("ref"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Project not found.")
		return
	} else if err != nil {
		s.internal(w, err)
		return
	}
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "db"
	}
	if !contains(logServices[p.Engine], service) {
		writeError(w, http.StatusBadRequest, "Unknown service.")
		return
	}
	text, err := s.prov.Logs(r.Context(), p.Ref, service, 300)
	if err != nil {
		writeError(w, http.StatusNotFound, "No logs yet for this service.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": logServices[p.Engine], "service": service, "text": text})
}
