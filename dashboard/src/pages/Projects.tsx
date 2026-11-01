import { useEffect, useState } from "react";
import { api, engineLabel, type Project } from "../api";
import { Box, Button, Card, Notice, PageHeader, StatusBadge } from "../ui";
import { navigate } from "../App";

// A short mark per engine for the card's icon tile.
const engineIcon: Record<Project["engine"], string> = { postgres: "PG", mysql: "MY", mongodb: "MG" };

export function Projects() {
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .projects()
        .then((p) => alive && (setProjects(p), setError("")))
        .catch((e) => alive && setError(e.message));
    load();
    const timer = setInterval(load, 3000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, []);

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Projects"
        action={
          <Button variant="primary" onClick={() => navigate("/new")}>
            <svg viewBox="0 0 20 20" className="size-3.5" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
              <path d="M10 4v12M4 10h12" />
            </svg>
            New project
          </Button>
        }
        subtitle="Each project is its own database with its own keys."
      />

      {error && <Notice tone="danger">{error}</Notice>}

      {projects && projects.length === 0 && (
        <Card className="flex flex-col items-center gap-2 px-6 py-12 text-center">
          <h2 className="h3">No projects yet</h2>
          <p className="body-sm text-ink-muted">Add one to get a Postgres, MySQL or MongoDB-compatible database.</p>
        </Card>
      )}

      {projects && projects.length > 0 && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {projects.map((p) => (
            <Box
              key={p.ref}
              href={`#/project/${p.ref}`}
              title={p.name}
              icon={engineIcon[p.engine]}
              detail={
                <>
                  {engineLabel[p.engine]} · {p.plan} · {p.region}
                </>
              }
              aside={
                <>
                  <StatusBadge status={p.status} />
                  <span className="truncate font-mono text-[11px] text-ink-muted">{p.ref}</span>
                </>
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}
