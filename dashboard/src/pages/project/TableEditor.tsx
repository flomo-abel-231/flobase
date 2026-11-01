import { useCallback, useEffect, useState, type FormEvent, type KeyboardEvent } from "react";
import { api, type Column, type NewColumn, type Project, type SchemaInfo, type TableInfo, type TableRows } from "../../api";
import { Button, Card, Field, Input, Notice } from "../../ui";

// The grid follows DataTables: entries per page, a search box across all
// columns, sortable headers, striped rows and numbered pages. Searching,
// sorting and paging run in the database, so large tables stay fast.
const pageSizes = [10, 25, 50, 100];

function storedPageSize(): number {
  try {
    const n = Number(localStorage.getItem("flobase.pageSize"));
    return pageSizes.includes(n) ? n : 25;
  } catch {
    return 25;
  }
}

// pageList is the numbered pages to show: first, last, and those around
// the current one, with gaps as null.
function pageList(current: number, count: number): (number | null)[] {
  const pages = new Set([0, count - 1, current - 1, current, current + 1].filter((p) => p >= 0 && p < count));
  const sorted = [...pages].sort((a, b) => a - b);
  const out: (number | null)[] = [];
  sorted.forEach((p, i) => {
    if (i > 0 && p - sorted[i - 1] > 1) out.push(null);
    out.push(p);
  });
  return out;
}

function csvCell(v: unknown): string {
  const t = display(v);
  return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t;
}

const typeOptions: { value: string; label: string }[] = [
  { value: "text", label: "text" },
  { value: "varchar", label: "varchar" },
  { value: "int4", label: "integer" },
  { value: "int8", label: "bigint" },
  { value: "float8", label: "double precision" },
  { value: "numeric", label: "numeric" },
  { value: "bool", label: "boolean" },
  { value: "uuid", label: "uuid" },
  { value: "timestamptz", label: "timestamptz" },
  { value: "date", label: "date" },
  { value: "jsonb", label: "jsonb" },
];

function display(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

// An empty field means NULL; Postgres converts the text to the column type.
function toValue(text: string): unknown {
  return text === "" ? null : text;
}

const quote = (name: string) => `"${name.replace(/"/g, '""')}"`;

// Only these schemas are served by the project's REST API, so only there
// does row-level security decide what the anon key can reach.
const apiSchemas = ["public"];

// The schema the editor shows is remembered per project in this browser.
const schemaKey = (ref: string) => `flobase.schema.${ref}`;
function rememberedSchema(ref: string): string {
  try {
    return localStorage.getItem(schemaKey(ref)) ?? "";
  } catch {
    return "";
  }
}

// TableEditor browses any schema, as Supabase's does: a schema picker, the
// schema's tables and views, and the chosen one's columns and rows.
export function TableEditor({ project }: { project: Project }) {
  const [schemas, setSchemas] = useState<SchemaInfo[] | null>(null);
  const [schema, setSchema] = useState("");
  const [tables, setTables] = useState<TableInfo[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");

  // Open on the remembered schema, else public if it has tables, else the
  // first of the project's own schemas that does.
  useEffect(() => {
    api
      .schemas(project.ref)
      .then((list) => {
        setSchemas(list);
        const remembered = rememberedSchema(project.ref);
        const has = (n: string) => list.some((x) => x.name === n);
        const own = list.filter((x) => !x.protected);
        const first = own.find((x) => x.name === "public" && x.tables > 0) ?? own.find((x) => x.tables > 0) ?? own.find((x) => x.name === "public");
        setSchema(remembered && has(remembered) ? remembered : first?.name ?? "public");
      })
      .catch((e) => setError((e as Error).message));
  }, [project.ref]);

  const pickSchema = (name: string) => {
    try {
      localStorage.setItem(schemaKey(project.ref), name);
    } catch {
      // Not remembered; fine.
    }
    setSchema(name);
    setSelected(null);
    setCreating(false);
  };

  const current = schemas?.find((x) => x.name === schema);

  const loadTables = useCallback(async (select?: string) => {
    if (!schema) return;
    try {
      const list = await api.tables(project.ref, schema);
      setTables(list);
      setSelected((cur) => select ?? (cur && list.some((t) => t.name === cur) ? cur : list[0]?.name ?? null));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [project.ref, schema]);

  useEffect(() => {
    loadTables();
  }, [loadTables]);

  return (
    <div className="grid gap-6 lg:grid-cols-[220px_1fr]">
      <aside className="flex flex-col gap-3">
        <label className="flex flex-col gap-1">
          <span className="label text-ink-muted">Schema</span>
          <select
            value={schema}
            onChange={(e) => pickSchema(e.target.value)}
            className="h-8 rounded-lg border border-border bg-surface-raised px-2 text-[13px] text-ink hover:border-border-strong"
          >
            {schemas
              ?.filter((x) => !x.protected)
              .map((x) => (
                <option key={x.name} value={x.name}>
                  {x.name} ({x.tables})
                </option>
              ))}
            {schemas?.some((x) => x.protected) && (
              <optgroup label="FloBase's own (read-only)">
                {schemas
                  .filter((x) => x.protected)
                  .map((x) => (
                    <option key={x.name} value={x.name}>
                      {x.name} ({x.tables})
                    </option>
                  ))}
              </optgroup>
            )}
          </select>
        </label>
        {!current?.protected && <Button onClick={() => setCreating(true)}>New table</Button>}
        <nav aria-label="Tables">
          <ul className="flex flex-col gap-0.5">
            {tables?.map((t) => (
              <li key={t.name}>
                <button
                  type="button"
                  onClick={() => {
                    setSelected(t.name);
                    setCreating(false);
                  }}
                  aria-current={t.name === selected && !creating ? "true" : undefined}
                  className={`flex w-full items-center justify-between gap-2 rounded-md px-3 py-2 text-left ${
                    t.name === selected && !creating ? "bg-brand-subtle text-ink" : "text-ink-muted hover:bg-surface-sunken hover:text-ink"
                  }`}
                >
                  <span className="code truncate">{t.name}</span>
                  {t.kind === "view" ? (
                    <span className="label shrink-0 text-ink-muted">view</span>
                  ) : (
                    !t.rls && apiSchemas.includes(schema) && <span className="label shrink-0 text-warning">RLS off</span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </nav>
        {tables && tables.length === 0 && <p className="body-sm text-ink-muted">No tables in the {schema} schema yet.</p>}
      </aside>

      <section className="min-w-0">
        {error && <Notice tone="danger">{error}</Notice>}
        {creating ? (
          <NewTable
            project={project}
            schema={schema}
            onCancel={() => setCreating(false)}
            onCreated={(name) => {
              setCreating(false);
              loadTables(name);
            }}
          />
        ) : selected ? (
          <TableView key={schema + "." + selected} project={project} schema={schema} table={selected} onChanged={() => loadTables()} />
        ) : null}
      </section>
    </div>
  );
}

function TableView({ project, schema, table, onChanged }: { project: Project; schema: string; table: string; onChanged: () => void }) {
  const [data, setData] = useState<TableRows | null>(null);
  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(storedPageSize);
  const [searchText, setSearchText] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<{ col: string; dir: "asc" | "desc" } | null>(null);
  const offset = page * pageSize;

  // Search after a short pause in typing, from the first page.
  useEffect(() => {
    const t = setTimeout(() => {
      setSearch(searchText.trim());
      setPage(0);
    }, 300);
    return () => clearTimeout(t);
  }, [searchText]);
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<{ row: number; col: string; value: string } | null>(null);
  const [adding, setAdding] = useState<Record<string, string> | null>(null);
  const [confirm, setConfirm] = useState<number | null>(null);

  const load = useCallback(async () => {
    try {
      setData(await api.rows(project.ref, schema, table, { offset, limit: pageSize, search, sort: sort?.col, dir: sort?.dir }));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }, [project.ref, schema, table, offset, pageSize, search, sort]);

  useEffect(() => {
    load();
  }, [load]);

  if (!data) return error ? <Notice tone="danger">{error}</Notice> : null;

  // Rows are editable when the table has a primary key and is not read-only.
  const pk = data.read_only ? [] : data.columns.filter((c) => c.primary);
  const match = (row: Record<string, unknown>) => Object.fromEntries(pk.map((c) => [c.name, row[c.name]]));
  const pageCount = Math.max(1, Math.ceil(data.filtered / pageSize));

  // Export CSV downloads the rows on this page, as shown.
  const exportCSV = () => {
    const lines = [data.columns.map((c) => csvCell(c.name)).join(",")];
    for (const row of data.rows) lines.push(data.columns.map((c) => csvCell(row[c.name])).join(","));
    const url = URL.createObjectURL(new Blob([lines.join("\n") + "\n"], { type: "text/csv" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `${schema}.${table}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const act = async (fn: () => Promise<unknown>) => {
    try {
      await fn();
      setError("");
      await load();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const saveEdit = () => {
    if (!editing) return;
    const row = data.rows[editing.row];
    const e = editing;
    setEditing(null);
    if (display(row[e.col]) === e.value) return;
    act(() => api.updateRow(project.ref, schema, table, match(row), { [e.col]: toValue(e.value) }));
  };

  const onEditKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") saveEdit();
    if (e.key === "Escape") setEditing(null);
  };

  const insert = (e: FormEvent) => {
    e.preventDefault();
    if (!adding) return;
    // Blank fields are left out so column defaults apply.
    const values = Object.fromEntries(Object.entries(adding).filter(([, v]) => v !== ""));
    setAdding(null);
    act(() => api.insertRow(project.ref, schema, table, values));
  };

  const enableRLS = () =>
    act(async () => {
      await api.sql(project.ref, `alter table ${quote(schema)}.${quote(table)} enable row level security;`);
      onChanged();
    });

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col">
          <h2 className="h3 code">
            <span className="text-ink-muted">{schema}.</span>
            {table}
          </h2>
          <span className="body-sm text-ink-muted">
            {data.view ? "View · " : ""}
            {data.total.toLocaleString()} rows · {data.columns.length} columns
            {data.read_only
              ? data.view
                ? " · read-only: a view has no rows of its own"
                : ` · read-only: the ${schema} schema belongs to FloBase's services`
              : pk.length === 0
                ? " · no primary key, so rows are read-only here"
                : ""}
          </span>
        </div>
        {!data.read_only && (
          <Button variant="primary" onClick={() => setAdding({})} disabled={adding !== null}>
            Insert row
          </Button>
        )}
      </div>

      {error && <Notice tone="danger">{error}</Notice>}
      {!data.rls && !data.read_only && apiSchemas.includes(schema) && (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-warning bg-surface-raised px-4 py-3">
          <p className="body-sm text-warning">
            <strong className="font-medium">Row-level security is off.</strong> Anyone with the anon key can read and change every
            row through the API.
          </p>
          <Button onClick={enableRLS}>Enable RLS</Button>
        </div>
      )}

      {adding && (
        <Card className="p-4">
          <form onSubmit={insert} className="flex flex-col gap-4">
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
              {data.columns.map((c) => (
                <Field key={c.name} label={c.name} hint={hint(c)}>
                  <Input
                    value={adding[c.name] ?? ""}
                    onChange={(e) => setAdding({ ...adding, [c.name]: e.target.value })}
                    placeholder={c.default ? "default" : c.nullable ? "NULL" : ""}
                  />
                </Field>
              ))}
            </div>
            <div className="flex gap-3">
              <Button type="submit">Save row</Button>
              <Button type="button" onClick={() => setAdding(null)}>
                Cancel
              </Button>
            </div>
          </form>
        </Card>
      )}

      <div className="flex flex-wrap items-center justify-between gap-3">
        <label className="body-sm flex items-center gap-2 text-ink-muted">
          Show
          <select
            value={pageSize}
            onChange={(e) => {
              const n = Number(e.target.value);
              setPageSize(n);
              setPage(0);
              try {
                localStorage.setItem("flobase.pageSize", String(n));
              } catch {
                // Not remembered; fine.
              }
            }}
            className="h-8 rounded-lg border border-border bg-surface-raised px-2 text-[13px] text-ink"
          >
            {pageSizes.map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          entries
        </label>
        <div className="flex items-center gap-2">
          <label className="body-sm flex items-center gap-2 text-ink-muted">
            Search
            <Input
              type="search"
              value={searchText}
              onChange={(e) => setSearchText(e.target.value)}
              placeholder="Any column…"
              aria-label="Search rows"
            />
          </label>
          <Button onClick={exportCSV} disabled={data.rows.length === 0} title="Download the rows on this page as CSV">
            Export CSV
          </Button>
        </div>
      </div>

      <div className="max-h-[65vh] overflow-auto rounded-xl border border-border bg-surface-raised shadow-sm">
        <table className="w-full border-collapse text-left">
          <thead className="sticky top-0 z-10 bg-surface-panel">
            <tr>
              {data.columns.map((c) => {
                const dir = sort?.col === c.name ? sort.dir : null;
                return (
                  <th
                    key={c.name}
                    aria-sort={dir === "asc" ? "ascending" : dir === "desc" ? "descending" : "none"}
                    className="whitespace-nowrap border-b border-border p-0"
                  >
                    <button
                      type="button"
                      onClick={() => {
                        setSort(dir === null ? { col: c.name, dir: "asc" } : dir === "asc" ? { col: c.name, dir: "desc" } : null);
                        setPage(0);
                      }}
                      title={`Sort by ${c.name}`}
                      className="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-surface-sunken"
                    >
                      <span className="label text-ink">{c.name}</span>
                      <span className="code text-ink-muted">{c.type}</span>
                      {c.primary && <span className="label text-ink-muted">PK</span>}
                      <span className={`ml-auto text-[10px] leading-none ${dir ? "text-brand" : "text-ink-muted/50"}`} aria-hidden>
                        {dir === "asc" ? "▲" : dir === "desc" ? "▼" : "▲▼"}
                      </span>
                    </button>
                  </th>
                );
              })}
              {pk.length > 0 && <th className="border-b border-border" aria-label="Actions" />}
            </tr>
          </thead>
          <tbody>
            {data.rows.map((row, i) => (
              <tr key={i} className="border-b border-border last:border-0 odd:bg-surface even:bg-surface-raised hover:bg-brand-subtle/40">
                {data.columns.map((c) => {
                  const isEditing = editing?.row === i && editing.col === c.name;
                  const v = row[c.name];
                  return (
                    <td key={c.name} className="code max-w-[280px] px-1 py-1 text-ink">
                      {isEditing ? (
                        <input
                          autoFocus
                          aria-label={`Edit ${c.name}`}
                          value={editing.value}
                          onChange={(e) => setEditing({ ...editing, value: e.target.value })}
                          onKeyDown={onEditKey}
                          onBlur={saveEdit}
                          className="code w-full min-w-[120px] rounded-sm border border-brand bg-surface-raised px-2 py-1 text-ink"
                        />
                      ) : (
                        <button
                          type="button"
                          disabled={pk.length === 0}
                          onClick={() => setEditing({ row: i, col: c.name, value: display(v) })}
                          title={pk.length ? "Click to edit" : display(v)}
                          className="block w-full truncate rounded-sm px-2 py-1 text-left enabled:hover:bg-surface-sunken"
                        >
                          {v === null || v === undefined ? <span className="text-ink-muted">NULL</span> : display(v)}
                        </button>
                      )}
                    </td>
                  );
                })}
                {pk.length > 0 && (
                  <td className="whitespace-nowrap px-2 py-1 text-right">
                    {confirm === i ? (
                      <span className="inline-flex gap-2">
                        <Button
                          variant="danger"
                          onClick={() => {
                            setConfirm(null);
                            act(() => api.deleteRow(project.ref, schema, table, match(row)));
                          }}
                        >
                          Delete
                        </Button>
                        <Button onClick={() => setConfirm(null)}>Cancel</Button>
                      </span>
                    ) : (
                      <Button onClick={() => setConfirm(i)} aria-label={`Delete row ${i + 1}`}>
                        Delete
                      </Button>
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
        {data.rows.length === 0 && (
          <p className="body-sm px-3 py-6 text-center text-ink-muted">{search ? "No matching rows." : "No rows yet."}</p>
        )}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="body-sm text-ink-muted">
          {data.filtered === 0
            ? "Showing 0 entries"
            : `Showing ${(offset + 1).toLocaleString()} to ${Math.min(offset + pageSize, data.filtered).toLocaleString()} of ${data.filtered.toLocaleString()} entries`}
          {search && data.filtered !== data.total ? ` (filtered from ${data.total.toLocaleString()} total)` : ""}
        </span>
        {pageCount > 1 && (
          <nav aria-label="Pages" className="flex items-center gap-1">
            <Button onClick={() => setPage(page - 1)} disabled={page === 0}>
              Previous
            </Button>
            {pageList(page, pageCount).map((p, i) =>
              p === null ? (
                <span key={`gap${i}`} className="px-1 text-ink-muted" aria-hidden>
                  …
                </span>
              ) : (
                <button
                  key={p}
                  type="button"
                  onClick={() => setPage(p)}
                  aria-current={p === page ? "page" : undefined}
                  className={`h-8 min-w-8 rounded-lg px-2 text-[13px] font-medium transition ${
                    p === page ? "bg-brand text-on-brand" : "text-ink hover:bg-surface-sunken"
                  }`}
                >
                  {p + 1}
                </button>
              ),
            )}
            <Button onClick={() => setPage(page + 1)} disabled={page >= pageCount - 1}>
              Next
            </Button>
          </nav>
        )}
      </div>
    </div>
  );
}

function hint(c: Column): string {
  if (c.default) return `Blank uses the default: ${c.default}`;
  return c.nullable ? "Blank means NULL." : "Required.";
}

function NewTable({
  project,
  schema,
  onCancel,
  onCreated,
}: {
  project: Project;
  schema: string;
  onCancel: () => void;
  onCreated: (name: string) => void;
}) {
  const [name, setName] = useState("");
  const [rls, setRls] = useState(true);
  const [cols, setCols] = useState<NewColumn[]>([{ name: "", type: "text", nullable: true }]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const update = (i: number, patch: Partial<NewColumn>) => setCols(cols.map((c, j) => (j === i ? { ...c, ...patch } : c)));

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.createTable(project.ref, schema, { name: name.trim(), rls, columns: cols.filter((c) => c.name.trim()) });
      onCreated(name.trim());
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  return (
    <Card className="p-6">
      <form onSubmit={submit} className="flex flex-col gap-6">
        <div className="flex flex-col gap-1">
          <h2 className="h3">New table</h2>
          <p className="body-sm text-ink-muted">
            Every table gets an <span className="font-mono">id</span> primary key and a{" "}
            <span className="font-mono">created_at</span> time.
          </p>
        </div>
        <Field label="Name" hint="Lowercase letters, digits and underscores.">
          <Input value={name} onChange={(e) => setName(e.target.value)} required pattern="[a-z][a-z0-9_]*" autoFocus />
        </Field>
        <label className="flex items-start gap-3">
          <input type="checkbox" checked={rls} onChange={(e) => setRls(e.target.checked)} className="mt-1 size-4 accent-brand" />
          <span className="flex flex-col">
            <span className="body text-ink">Enable row-level security</span>
            <span className="body-sm text-ink-muted">
              Recommended. With it on, the API returns no rows until you add a policy.
            </span>
          </span>
        </label>
        <fieldset className="flex flex-col gap-3">
          <legend className="label mb-2 text-ink">Columns</legend>
          {cols.map((c, i) => (
            <div key={i} className="flex flex-wrap items-center gap-3">
              <div className="min-w-[160px] flex-1">
                <Input
                  aria-label={`Column ${i + 1} name`}
                  placeholder="column_name"
                  value={c.name}
                  onChange={(e) => update(i, { name: e.target.value })}
                  pattern="[a-z][a-z0-9_]*"
                />
              </div>
              <select
                aria-label={`Column ${i + 1} type`}
                value={c.type}
                onChange={(e) => update(i, { type: e.target.value })}
                className="h-9 rounded-md border border-border-strong bg-surface-raised px-2 text-[15px] text-ink"
              >
                {typeOptions.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
              <label className="body-sm flex items-center gap-2 text-ink">
                <input
                  type="checkbox"
                  checked={c.nullable}
                  onChange={(e) => update(i, { nullable: e.target.checked })}
                  className="size-4 accent-brand"
                />
                Nullable
              </label>
              <Button type="button" onClick={() => setCols(cols.filter((_, j) => j !== i))} aria-label={`Remove column ${i + 1}`}>
                Remove
              </Button>
            </div>
          ))}
          <div>
            <Button type="button" onClick={() => setCols([...cols, { name: "", type: "text", nullable: true }])}>
              Add column
            </Button>
          </div>
        </fieldset>
        {error && <Notice tone="danger">{error}</Notice>}
        <div className="flex gap-3">
          <Button variant="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Creating…" : "Create table"}
          </Button>
          <Button type="button" onClick={onCancel}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
