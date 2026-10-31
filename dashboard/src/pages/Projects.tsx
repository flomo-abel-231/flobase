import { useEffect, useState } from "react";
import { api, engineLabel, type Project } from "../api";
import { Box, Button, Card, Notice, PageHeader, StatusBadge } from "../ui";
import { navigate } from "../App";

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
            + Add
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
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {projects.map((p) => (
            <Box
              key={p.ref}
              href={`#/project/${p.ref}`}
              title={p.name}
              aside={<StatusBadge status={p.status} />}
              detail={
                <>
                  {engineLabel[p.engine]} · {p.plan} · {p.region} · <span className="font-mono">{p.ref}</span>
                </>
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}
