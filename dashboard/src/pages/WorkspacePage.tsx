import { useState, type FormEvent } from "react";
import { api, type Me } from "../api";
import { Button, Card, Field, Input, Notice, PageHeader } from "../ui";
import { StorageUsage } from "./StorageUsage";

// The current workspace's name, a form for a new workspace, and deleting
// one that has no projects left. Each workspace is its own organization.
export function WorkspacePage({ me, onChanged }: { me: Me; onChanged: (switchTo?: string) => void }) {
  const current = me.workspaces.find((w) => w.id === me.workspace);
  const canManage = current?.role === "owner" || current?.role === "admin";

  return (
    <div className="flex max-w-3xl flex-col gap-8">
      <PageHeader
        title={current ? current.name : "Workspaces"}
        subtitle="A workspace is one organization: its own projects, members and assistant. Nothing is shared between workspaces."
      />
      {current && <StorageUsage />}
      {current && canManage && <Rename id={current.id} name={current.name} onDone={() => onChanged()} />}
      <Create onCreated={(id) => onChanged(id)} required={!current} />
      {current?.role === "owner" && <Delete id={current.id} name={current.name} onDone={() => onChanged("")} />}
    </div>
  );
}

function Rename({ id, name, onDone }: { id: string; name: string; onDone: () => void }) {
  const [value, setValue] = useState(name);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.renameWorkspace(id, value.trim());
      setError("");
      onDone();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-4">
        <Field label="Workspace name">
          <Input value={value} onChange={(e) => setValue(e.target.value)} maxLength={64} required />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <div>
          <Button type="submit" disabled={busy || value.trim() === name || !value.trim()}>
            Save
          </Button>
        </div>
      </form>
    </Card>
  );
}

function Create({ onCreated, required }: { onCreated: (id: string) => void; required: boolean }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const save = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const w = await api.createWorkspace(name.trim());
      onCreated(w.id);
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };
  return (
    <Card className="p-5">
      <form onSubmit={save} className="flex flex-col gap-4">
        <h2 className="text-[14px] font-semibold text-ink">{required ? "Create a workspace to start" : "New workspace"}</h2>
        <Field label="Name" hint="For another company or client. You are its owner.">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={64} required placeholder="Acme Inc." />
        </Field>
        {error && <Notice tone="danger">{error}</Notice>}
        <div>
          <Button variant="primary" type="submit" disabled={busy || !name.trim()}>
            {busy ? "Creating…" : "Create workspace"}
          </Button>
        </div>
      </form>
    </Card>
  );
}

function Delete({ id, name, onDone }: { id: string; name: string; onDone: () => void }) {
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const remove = async () => {
    try {
      await api.deleteWorkspace(id);
      onDone();
    } catch (err) {
      setError((err as Error).message);
    }
  };
  return (
    <Card className="flex flex-col gap-3 p-5">
      <h2 className="text-[14px] font-semibold text-ink">Delete workspace</h2>
      <p className="body-sm text-ink-muted">
        Removes the workspace, its members' access, invites, assistant models, conversations and automations. Delete its
        projects first.
      </p>
      <Field label={`Type ${name} to confirm`}>
        <Input value={confirm} onChange={(e) => setConfirm(e.target.value)} />
      </Field>
      {error && <Notice tone="danger">{error}</Notice>}
      <div>
        <Button variant="danger" disabled={confirm !== name} onClick={remove}>
          Delete workspace
        </Button>
      </div>
    </Card>
  );
}
