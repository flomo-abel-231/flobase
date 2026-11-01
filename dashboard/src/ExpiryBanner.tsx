import { useEffect, useState } from "react";
import { api, daysUntil, type Project } from "./api";
import { navigate } from "./App";

// ExpiryBanner warns about free cloud projects that will be deleted within
// 15 days, the same window the warning emails start in.
export function ExpiryBanner({ workspace }: { workspace: string }) {
  const [projects, setProjects] = useState<Project[]>([]);

  useEffect(() => {
    let live = true;
    const load = () =>
      api
        .projects()
        .then((list) => live && setProjects(list))
        .catch(() => {});
    load();
    const t = setInterval(load, 10 * 60 * 1000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [workspace]);

  const soon = projects
    .filter((p) => p.expires_at && p.status !== "deleting" && daysUntil(p.expires_at) <= 15)
    .sort((a, b) => a.expires_at!.localeCompare(b.expires_at!));
  if (soon.length === 0) return null;

  return (
    <div className="flex flex-col gap-2" role="alert">
      {soon.map((p) => {
        const days = Math.max(daysUntil(p.expires_at!), 0);
        const urgent = days <= 3;
        return (
          <div
            key={p.ref}
            className={`flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3 body-sm ${
              urgent ? "border-danger bg-danger/10 text-danger" : "border-warning bg-warning/10 text-warning"
            }`}
          >
            <span>
              <strong className="font-medium">
                {p.name} will be deleted {days <= 0 ? "today" : days === 1 ? "tomorrow" : `in ${days} days`}
              </strong>{" "}
              ({new Date(p.expires_at!).toLocaleDateString(undefined, { day: "numeric", month: "long" })}). Free projects are deleted
              with their data after 30 days; export what you want to keep, or move the project to another plan.
            </span>
            <button
              type="button"
              className="rounded-md border border-current px-2.5 py-1 font-medium hover:bg-current/10"
              onClick={() => navigate(`/project/${p.ref}/configuration`)}
            >
              Open project
            </button>
          </div>
        );
      })}
    </div>
  );
}
