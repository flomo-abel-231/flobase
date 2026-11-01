package api

import (
	"strings"
	"testing"
)

func TestSQLRisk(t *testing.T) {
	cases := map[string]string{
		"select * from orders limit 10":                         riskRead,
		"SELECT count(*) FROM users; select now()":              riskRead,
		"with t as (select 1) select * from t":                  riskRead,
		"explain select 1":                                      riskRead,
		"show tables":                                           riskRead,
		"select updated_at, created_at from orders":             riskRead,
		"explain analyze select 1":                              riskChange,
		"insert into t values (1)":                              riskChange,
		"select 1; update t set a = 1":                          riskChange,
		"with d as (delete from t returning *) select * from d": riskDelete,
		"select pg_terminate_backend(123)":                      riskChange,
		"select * into backup_t from t":                         riskChange,
		"select * from t for update":                            riskChange,
		"set statement_timeout = 0; select 1":                   riskChange,
		"drop table t":                                          riskDelete,
		"truncate t":                                            riskDelete,
		"alter table t drop column a":                           riskDelete,
		"create table t (id int)":                               riskChange,
		"select '--'; drop table t":                             riskDelete,
		"":                                                      riskChange,
	}
	for q, want := range cases {
		if got := sqlRisk(q); got != want {
			t.Errorf("sqlRisk(%q) = %s, want %s", q, got, want)
		}
	}
}

func TestDecision(t *testing.T) {
	cases := []struct {
		mode, risk string
		automation bool
		want       string
	}{
		{modeRead, riskRead, false, "run"},
		{modeRead, riskChange, false, "refuse"},
		{modeRead, riskDelete, false, "refuse"},
		{modeAsk, riskChange, false, "ask"},
		{modeAsk, riskDelete, false, "ask"},
		{modeAsk, riskChange, true, "refuse"},
		{modeAuto, riskChange, false, "run"},
		{modeAuto, riskChange, true, "run"},
		{modeAuto, riskDelete, false, "ask"},
		{modeAuto, riskDelete, true, "refuse"},
	}
	for _, c := range cases {
		if got := decision(c.mode, c.risk, c.automation); got != c.want {
			t.Errorf("decision(%s, %s, %v) = %s, want %s", c.mode, c.risk, c.automation, got, c.want)
		}
	}
}

func TestRedact(t *testing.T) {
	v := map[string]any{
		"name":       "shop",
		"connection": map[string]any{"host": "db", "password": "secret", "uri": "postgres://u:secret@db/x"},
		"keys":       map[string]any{"anon": "a", "service_role": "b"},
		"list":       []any{map[string]any{"password": "p"}},
	}
	out := string(mustJSON(redact(v)))
	for _, secret := range []string{"secret", `"a"`, `"b"`, `"p"`} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted output still holds %s: %s", secret, out)
		}
	}
}
