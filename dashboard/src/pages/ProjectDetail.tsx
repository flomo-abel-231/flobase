import { useEffect, useState } from "react";
import { api, ApiError, engineLabel, type Project } from "../api";
import { Notice, StatusBadge } from "../ui";
import { navigate } from "../App";
import { Configuration } from "./project/Configuration";
import { SqlEditor } from "./project/SqlEditor";
import { TableEditor } from "./project/TableEditor";
import { AuthUsers } from "./project/AuthUsers";
import { Logs } from "./project/Logs";
import { Backups } from "./project/Backups";

// A project page in the shape of a Coolify resource page: breadcrumbs and
// status, a row of tabs, then the tab's content.
const tabs: { id: string; label: string; engines: Project["engine"][] }[] = [
  { id: "configuration", label: "Configuration", engines: ["postgres", "mysql", "mongodb"] },
  { id: "tables", label: "Table editor", engines: ["postgres"] },
  { id: "sql", label: "SQL editor", engines: ["postgres", "mysql"] },
  { id: "auth", label: "Authentication", engines: ["postgres"] },
  { id: "backups", label: "Backups", engines: ["postgres"] },
  { id: "logs", label: "Logs", engines: ["postgres", "mysql", "mongodb"] },
];

export function ProjectDetail({ projectRef, tab, section }: { projectRef: string; tab: string; section?: string }) {
  const [project, setProject] = useState<Project | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const load = async () => {
      try {
        const p = await api.project(projectRef);
        if (!alive) return;
        setProject(p);
        setError("");
        if (p.status === "provisioning" || p.status === "deleting") timer = setTimeout(load, 2000);
      } catch (e) {
        if (!alive) return;
        if (e instanceof ApiError && e.status === 404) {
          navigate("/");
          return;
        }
        setError((e as Error).message);
      }
    };
    load();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [projectRef]);

  if (!project) return error ? <Notice tone="danger">{error}</Notice> : null;

  const available = tabs.filter((t) => t.engines.includes(project.engine));
  const current = available.find((t) => t.id === tab) ?? available[0];
  const active = project.status === "active";

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-2">
        <nav aria-label="Breadcrumb" className="body-sm flex flex-wrap items-center gap-1.5 text-ink-muted">
          <a href="#/" className="hover:text-ink">
            Projects
          </a>
          <span aria-hidden>›</span>
          <span className="text-ink">{project.name}</span>
          <span aria-hidden>›</span>
          <span>{engineLabel[project.engine]}</span>
        </nav>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="h2 min-w-0 break-words">{project.name}</h1>
          <StatusBadge status={project.status} />
        </div>
        <p className="body-sm text-ink-muted">
          {project.plan} · {project.region} · <span className="font-mono">{project.ref}</span>
        </p>
      </div>

      <nav aria-label="Project" className="-mx-4 overflow-x-auto border-b border-border px-4 sm:mx-0 sm:px-0">
        <ul className="flex gap-4">
          {available.map((t) => (
            <li key={t.id}>
              <a
                href={`#/project/${project.ref}/${t.id}`}
                aria-current={t.id === current.id ? "page" : undefined}
                className={`-mb-px inline-block whitespace-nowrap border-b-2 py-2 text-[13px] font-medium ${
                  t.id === current.id ? "border-brand text-ink" : "border-transparent text-ink-muted hover:text-ink"
                }`}
              >
                {t.label}
              </a>
            </li>
          ))}
        </ul>
      </nav>

      {error && <Notice tone="danger">{error}</Notice>}
      {project.status === "failed" && <Notice tone="danger">{project.status_detail || "Provisioning failed."}</Notice>}
      {project.status === "provisioning" && (
        <Notice tone="warning">Starting the database and its services. The first project on an engine also downloads its images.</Notice>
      )}

      {current.id === "configuration" && <Configuration project={project} section={section} />}
      {current.id !== "configuration" && !active && <p className="body-sm text-ink-muted">Available once the project is active.</p>}
      {active && current.id === "sql" && <SqlEditor project={project} />}
      {active && current.id === "tables" && <TableEditor project={project} />}
      {active && current.id === "auth" && <AuthUsers project={project} />}
      {active && current.id === "backups" && <Backups project={project} />}
      {active && current.id === "logs" && <Logs project={project} />}
    </div>
  );
}
