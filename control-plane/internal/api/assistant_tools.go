package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"

	"flobase/control-plane/internal/llm"
)

// The assistant's tools call FloBase's own API inside the process, so they
// get the same checks as the dashboard. Secrets are removed from what goes
// back to the model.

type assistantTool struct {
	llm.Tool
	// risk is read, change or delete; sql tools decide per query.
	risk func(in toolInput) string
	// title says what the call does, for the dashboard.
	title func(in toolInput) (title, detail string)
	run   func(ctx context.Context, in toolInput) (string, error)
}

type toolInput map[string]any

func (in toolInput) str(key string) string {
	v, _ := in[key].(string)
	return strings.TrimSpace(v)
}

func fixed(risk string) func(toolInput) string { return func(toolInput) string { return risk } }

var (
	refPattern     = regexp.MustCompile(`^[a-z]{1,40}$`)
	uuidPattern    = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
	servicePattern = regexp.MustCompile(`^[a-z-]{1,20}$`)
)

func props(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

var refProp = strProp("The project's ref, a short lowercase id from list_projects.")

func (s *Server) tools() []assistantTool {
	ref := func(in toolInput) (string, error) {
		r := in.str("ref")
		if !refPattern.MatchString(r) {
			return "", errors.New("Give a project ref from list_projects.")
		}
		return r, nil
	}
	onRef := func(verb string) func(toolInput) (string, string) {
		return func(in toolInput) (string, string) { return verb + " " + in.str("ref"), "" }
	}
	get := func(path func(toolInput) (string, error)) func(context.Context, toolInput) (string, error) {
		return func(ctx context.Context, in toolInput) (string, error) {
			p, err := path(in)
			if err != nil {
				return "", err
			}
			return s.selfCall(ctx, http.MethodGet, p, nil)
		}
	}
	projectPath := func(suffix string) func(toolInput) (string, error) {
		return func(in toolInput) (string, error) {
			r, err := ref(in)
			return "/api/v1/projects/" + r + suffix, err
		}
	}

	return []assistantTool{
		{
			Tool:  llm.Tool{Name: "list_projects", Description: "List every project on this FloBase with its ref, engine, plan, region and status."},
			risk:  fixed(riskRead),
			title: func(toolInput) (string, string) { return "List projects", "" },
			run:   get(func(toolInput) (string, error) { return "/api/v1/projects", nil }),
		},
		{
			Tool:  llm.Tool{Name: "get_project", Description: "Get one project's details: engine, plan, status, host and API URL.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskRead),
			title: onRef("Look at project"),
			run:   get(projectPath("")),
		},
		{
			Tool:  llm.Tool{Name: "get_catalog", Description: "List the engines, plans (with prices) and regions a new project can use."},
			risk:  fixed(riskRead),
			title: func(toolInput) (string, string) { return "Look at engines, plans and regions", "" },
			run:   get(func(toolInput) (string, error) { return "/api/v1/catalog", nil }),
		},
		{
			Tool: llm.Tool{Name: "create_project", Description: "Create a project. Use get_catalog for valid engine, plan and region ids. Provisioning continues in the background; check with get_project.",
				Properties: props("name", strProp("A name for the project."), "engine", strProp("postgres, mysql or mongodb."),
					"plan", strProp("A plan id from get_catalog."), "region", strProp("A region id from get_catalog.")),
				Required: []string{"name", "engine", "plan", "region"}},
			risk: fixed(riskChange),
			title: func(in toolInput) (string, string) {
				return "Create project " + in.str("name"), fmt.Sprintf("%s · %s plan · %s", in.str("engine"), in.str("plan"), in.str("region"))
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects", map[string]string{
					"name": in.str("name"), "engine": in.str("engine"), "plan": in.str("plan"), "region": in.str("region")})
			},
		},
		{
			Tool:  llm.Tool{Name: "delete_project", Description: "Delete a project, its database and its local backups. Copies in off-site storage stay.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskDelete),
			title: onRef("Delete project"),
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				p, err := s.store.Project(ctx, r)
				if ws, _ := ctx.Value(toolWorkspaceKey{}).(string); err != nil || p.WorkspaceID != ws {
					return "", errors.New("Project not found.")
				}
				out, err := s.selfCall(ctx, http.MethodDelete, "/api/v1/projects/"+r, map[string]string{"confirm": p.Name})
				if err == nil && out == "Done." {
					out = "Deleting " + p.Name + "."
				}
				return out, err
			},
		},
		{
			Tool: llm.Tool{Name: "run_sql", Description: "Run SQL on a Postgres or MySQL project as the database owner. Several statements may be sent at once. Results are capped at 1000 rows per statement; use LIMIT.",
				Properties: props("ref", refProp, "query", strProp("The SQL to run.")), Required: []string{"ref", "query"}},
			risk: func(in toolInput) string { return sqlRisk(in.str("query")) },
			title: func(in toolInput) (string, string) {
				return "Run SQL on " + in.str("ref"), in.str("query")
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				q := in.str("query")
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects/"+r+"/sql", map[string]any{"query": q, "read_only": sqlRisk(q) == riskRead})
			},
		},
		{
			Tool:  llm.Tool{Name: "list_schemas", Description: "List a Postgres project's schemas with how many tables each has. Protected ones belong to FloBase (auth, storage and the like) and are read-only.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskRead),
			title: onRef("List schemas in"),
			run:   get(projectPath("/schemas")),
		},
		{
			Tool: llm.Tool{Name: "list_tables", Description: "List a Postgres project's tables and views in one schema (public unless given), with row estimates and whether row-level security is on. A row_estimate of null means not yet counted, not empty; use run_sql with count(*) for exact numbers. If the schema has no tables, call list_schemas before saying the database is empty.",
				Properties: props("ref", refProp, "schema", strProp("A schema name; default public.")), Required: []string{"ref"}},
			risk:  fixed(riskRead),
			title: onRef("List tables in"),
			run: get(func(in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				p := "/api/v1/projects/" + r + "/tables"
				if sc := in.str("schema"); sc != "" {
					p += "?schema=" + url.QueryEscape(sc)
				}
				return p, nil
			}),
		},
		{
			Tool:  llm.Tool{Name: "list_auth_users", Description: "List a Postgres project's auth users (Supabase Auth).", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskRead),
			title: onRef("List auth users in"),
			run:   get(projectPath("/users")),
		},
		{
			Tool: llm.Tool{Name: "create_auth_user", Description: "Create a confirmed auth user with an email and password in a Postgres project.",
				Properties: props("ref", refProp, "email", strProp("The user's email."), "password", strProp("At least 6 characters.")), Required: []string{"ref", "email", "password"}},
			risk: fixed(riskChange),
			title: func(in toolInput) (string, string) {
				return "Add auth user " + in.str("email") + " to " + in.str("ref"), ""
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects/"+r+"/users", map[string]string{"email": in.str("email"), "password": in.str("password")})
			},
		},
		{
			Tool: llm.Tool{Name: "delete_auth_user", Description: "Delete an auth user from a Postgres project by id (from list_auth_users).",
				Properties: props("ref", refProp, "user_id", strProp("The user's id.")), Required: []string{"ref", "user_id"}},
			risk: fixed(riskDelete),
			title: func(in toolInput) (string, string) {
				return "Delete auth user from " + in.str("ref"), in.str("user_id")
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				id := in.str("user_id")
				if !uuidPattern.MatchString(id) {
					return "", errors.New("Give a user id from list_auth_users.")
				}
				return s.selfCall(ctx, http.MethodDelete, "/api/v1/projects/"+r+"/users/"+id, nil)
			},
		},
		{
			Tool:  llm.Tool{Name: "list_backups", Description: "List a Postgres project's full backups, the window it can be restored to, and the state of its off-site copies.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskRead),
			title: onRef("List backups of"),
			run:   get(projectPath("/backups")),
		},
		{
			Tool:  llm.Tool{Name: "backup_now", Description: "Take a full backup of a Postgres project now.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskChange),
			title: onRef("Back up"),
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects/"+r+"/backups", nil)
			},
		},
		{
			Tool: llm.Tool{Name: "restore_project", Description: "Restore a Postgres project into a NEW project; the original is not changed. Restores the latest state unless a time (RFC 3339) or a backup name is given.",
				Properties: props("ref", refProp, "time", strProp("Optional point in time, RFC 3339."), "backup", strProp("Optional backup name from list_backups."), "name", strProp("Optional name for the new project.")),
				Required:   []string{"ref"}},
			risk: fixed(riskChange),
			title: func(in toolInput) (string, string) {
				to := "latest state"
				if t := in.str("time"); t != "" {
					to = t
				} else if b := in.str("backup"); b != "" {
					to = "backup " + b
				}
				return "Restore " + in.str("ref") + " into a new project", "To " + to
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				body := map[string]any{"name": in.str("name")}
				switch {
				case in.str("time") != "":
					body["time"] = in.str("time")
				case in.str("backup") != "":
					body["backup"] = in.str("backup")
				default:
					body["latest"] = true
				}
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects/"+r+"/restore", body)
			},
		},
		{
			Tool: llm.Tool{Name: "get_logs", Description: "Read the last 300 log lines of a project service. Postgres projects have db, pooler, rest, auth and offsite; MySQL has db; MongoDB-compatible has db and pg.",
				Properties: props("ref", refProp, "service", strProp("The service; db when unsure.")), Required: []string{"ref"}},
			risk: fixed(riskRead),
			title: func(in toolInput) (string, string) {
				return "Read " + or(in.str("service"), "db") + " logs of " + in.str("ref"), ""
			},
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				svc := or(in.str("service"), "db")
				if !servicePattern.MatchString(svc) {
					return "", errors.New("Unknown service.")
				}
				return s.selfCall(ctx, http.MethodGet, "/api/v1/projects/"+r+"/logs?service="+svc, nil)
			},
		},
		{
			Tool:  llm.Tool{Name: "list_offsite_backups", Description: "List the projects with copies in off-site (S3) storage, including deleted ones that can be restored."},
			risk:  fixed(riskRead),
			title: func(toolInput) (string, string) { return "List off-site backups", "" },
			run:   get(func(toolInput) (string, error) { return "/api/v1/offsite", nil }),
		},
		{
			Tool:  llm.Tool{Name: "backup_to_cloud", Description: "Take a backup of a Postgres project and copy it to the active off-site storage location now.", Properties: props("ref", refProp), Required: []string{"ref"}},
			risk:  fixed(riskChange),
			title: onRef("Back up to cloud storage:"),
			run: func(ctx context.Context, in toolInput) (string, error) {
				r, err := ref(in)
				if err != nil {
					return "", err
				}
				return s.selfCall(ctx, http.MethodPost, "/api/v1/projects/"+r+"/backups/offsite", nil)
			},
		},
	}
}

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *Server) tool(name string) (assistantTool, bool) {
	for _, t := range s.tools() {
		if t.Name == name {
			return t, true
		}
	}
	return assistantTool{}, false
}

func parseInput(call llm.Call) (toolInput, error) {
	in := toolInput{}
	if len(call.Input) == 0 {
		return in, nil
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return nil, errors.New("The tool input was not valid JSON.")
	}
	return in, nil
}

// describeCall works out what a call is and how risky, for the dashboard
// and the permission check. Unknown or malformed calls count as changes.
func (s *Server) describeCall(call llm.Call) *toolEntry {
	te := &toolEntry{CallID: call.ID, Name: call.Name, Title: call.Name, Risk: riskChange}
	t, ok := s.tool(call.Name)
	in, err := parseInput(call)
	if !ok || err != nil {
		return te
	}
	te.Title, te.Detail = t.title(in)
	te.Risk = t.risk(in)
	return te
}

func (s *Server) runTool(ctx context.Context, call llm.Call) (string, error) {
	t, ok := s.tool(call.Name)
	if !ok {
		return "", errors.New("There is no tool called " + call.Name + ".")
	}
	in, err := parseInput(call)
	if err != nil {
		return "", err
	}
	out, err := t.run(ctx, in)
	return clip(out, 20000), err
}

// toolWorkspaceKey carries the workspace a conversation's tools act in.
type toolWorkspaceKey struct{}

// selfCall makes a request to this server's API and returns the response
// body with secrets removed; a failed request returns its message as the
// error.
func (s *Server) selfCall(ctx context.Context, method, path string, body any) (string, error) {
	s.assistant.once.Do(func() { s.assistant.self = s.Routes() })
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(mustJSON(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, reader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIToken)
	// The API token with a workspace header can reach that workspace only.
	ws, _ := ctx.Value(toolWorkspaceKey{}).(string)
	if ws == "" {
		return "", errors.New("the assistant has no workspace to work in")
	}
	req.Header.Set(workspaceHeader, ws)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.assistant.self.ServeHTTP(rec, req)
	out := rec.Body.Bytes()
	if rec.Code >= 300 {
		var e struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
			Hint   string `json:"hint"`
		}
		if json.Unmarshal(out, &e) == nil && e.Error != "" {
			msg := e.Error
			for _, extra := range []string{e.Detail, e.Hint} {
				if extra != "" {
					msg += " " + extra
				}
			}
			return "", errors.New(msg)
		}
		return "", fmt.Errorf("request failed with status %d", rec.Code)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return "Done.", nil
	}
	var v any
	if err := json.Unmarshal(out, &v); err != nil {
		return string(out), nil
	}
	return string(mustJSON(redact(v))), nil
}

// Secrets the model never sees.
var secretKeys = map[string]bool{
	"password": true, "uri": true, "pooler_uri": true, "keys": true,
	"anon": true, "service_role": true, "jwt_secret": true,
}

func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if secretKeys[k] {
				x[k] = "[hidden]"
			} else {
				x[k] = redact(val)
			}
		}
	case []any:
		for i := range x {
			x[i] = redact(x[i])
		}
	}
	return v
}

var (
	sqlWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_$]*`)
	// Statements that only read. Anything else is a change.
	sqlReadStart = map[string]bool{"select": true, "with": true, "show": true, "explain": true, "describe": true,
		"desc": true, "values": true, "table": true}
	sqlDeleteWords = map[string]bool{"drop": true, "truncate": true, "delete": true, "purge": true}
	sqlChangeWords = map[string]bool{
		"insert": true, "update": true, "create": true, "alter": true, "grant": true, "revoke": true, "merge": true,
		"replace": true, "rename": true, "comment": true, "copy": true, "call": true, "do": true, "execute": true,
		"exec": true, "set": true, "reset": true, "begin": true, "commit": true, "rollback": true, "start": true,
		"savepoint": true, "release": true, "lock": true, "unlock": true, "vacuum": true, "analyze": true,
		"analyse": true, "cluster": true, "reindex": true, "refresh": true, "notify": true, "listen": true,
		"discard": true, "load": true, "handler": true, "install": true, "uninstall": true, "kill": true,
		"flush": true, "optimize": true, "repair": true, "prepare": true, "deallocate": true, "import": true,
		"checkpoint": true, "into": true, "outfile": true, "dumpfile": true, "nextval": true, "setval": true,
		"set_config": true, "dblink": true, "dblink_exec": true, "pg_sleep": true, "sleep": true, "benchmark": true,
		"get_lock": true, "for": true, "security": true, "transaction": true,
	}
)

// sqlRisk sorts a query into read, change or delete by its words. It errs
// towards change: a word like "update" anywhere, even in a string or a
// comment, makes the query a change. Queries marked read also run in a
// read-only transaction, so the database refuses a change the words missed.
func sqlRisk(q string) string {
	words := sqlWord.FindAllString(q, -1)
	if len(words) == 0 {
		return riskChange
	}
	risk := riskRead
	if !sqlReadStart[strings.ToLower(words[0])] {
		risk = riskChange
	}
	for _, w := range words {
		lw := strings.ToLower(w)
		switch {
		case sqlDeleteWords[lw]:
			return riskDelete
		case sqlChangeWords[lw],
			strings.HasPrefix(lw, "pg_") && (strings.Contains(lw, "terminate") || strings.Contains(lw, "cancel") ||
				strings.Contains(lw, "reload") || strings.Contains(lw, "rotate") || strings.Contains(lw, "promote") ||
				strings.Contains(lw, "signal") || strings.Contains(lw, "file") || strings.Contains(lw, "advisory")),
			strings.HasPrefix(lw, "lo_"):
			risk = riskChange
		}
	}
	// Each statement must start with a read keyword.
	for _, stmt := range strings.Split(q, ";") {
		first := sqlWord.FindString(stmt)
		if first != "" && !sqlReadStart[strings.ToLower(first)] {
			risk = riskChange
		}
	}
	return risk
}
