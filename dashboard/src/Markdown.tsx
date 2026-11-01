import type { ReactNode } from "react";

// Markdown for the assistant's answers: paragraphs, headings, lists, code
// blocks, tables, bold, italics, inline code and links. It builds elements
// and never injects HTML, so text from a model or a database stays text.

function inline(text: string, key = 0): ReactNode[] {
  const out: ReactNode[] = [];
  const pattern = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(\[[^\]]+\]\((https?:\/\/[^)\s]+|#\/[^)\s]*)\))/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = pattern.exec(text))) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const k = `${key}-${m.index}`;
    if (m[1]) {
      out.push(
        <code key={k} className="code rounded-sm bg-surface-sunken px-1 py-0.5 text-ink">
          {m[1].slice(1, -1)}
        </code>,
      );
    } else if (m[2]) {
      out.push(<strong key={k}>{m[2].slice(2, -2)}</strong>);
    } else if (m[3]) {
      out.push(<em key={k}>{m[3].slice(1, -1)}</em>);
    } else if (m[4]) {
      const label = m[4].slice(1, m[4].indexOf("]"));
      const href = m[5];
      out.push(
        <a key={k} href={href} className="text-brand underline-offset-2 hover:underline" {...(href.startsWith("http") ? { target: "_blank", rel: "noreferrer" } : {})}>
          {label}
        </a>,
      );
    }
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

const isTableRow = (line: string) => /^\s*\|.*\|\s*$/.test(line);
const isDivider = (line: string) => /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(line);
const cells = (line: string) =>
  line
    .trim()
    .replace(/^\|/, "")
    .replace(/\|$/, "")
    .split("|")
    .map((c) => c.trim());

export function Markdown({ text }: { text: string }) {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const key = blocks.length;

    if (line.startsWith("```")) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) body.push(lines[i++]);
      i++;
      blocks.push(
        <pre key={key} className="code overflow-x-auto rounded-sm border border-border bg-surface-sunken p-3 text-ink">
          {body.join("\n")}
        </pre>,
      );
      continue;
    }

    if (isTableRow(line) && i + 1 < lines.length && isDivider(lines[i + 1])) {
      const head = cells(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && isTableRow(lines[i])) rows.push(cells(lines[i++]));
      blocks.push(
        <div key={key} className="overflow-x-auto rounded-sm border border-border">
          <table className="w-full border-collapse text-left text-[13px]">
            <thead className="bg-surface-sunken">
              <tr>
                {head.map((h, j) => (
                  <th key={j} className="border-b border-border px-2.5 py-1.5 font-medium text-ink-muted">
                    {inline(h)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r, j) => (
                <tr key={j} className="border-b border-border last:border-0">
                  {r.map((c, k) => (
                    <td key={k} className="px-2.5 py-1.5 align-top text-ink">
                      {inline(c)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>,
      );
      continue;
    }

    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      blocks.push(
        <p key={key} className="text-[14px] font-semibold text-ink">
          {inline(heading[2])}
        </p>,
      );
      i++;
      continue;
    }

    if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
      const ordered = /^\s*\d+\./.test(line);
      const items: string[] = [];
      while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) items.push(lines[i++].replace(/^\s*([-*]|\d+\.)\s+/, ""));
      const List = ordered ? "ol" : "ul";
      blocks.push(
        <List key={key} className={`flex flex-col gap-1 pl-5 ${ordered ? "list-decimal" : "list-disc"}`}>
          {items.map((it, j) => (
            <li key={j}>{inline(it, j)}</li>
          ))}
        </List>,
      );
      continue;
    }

    if (line.trim() === "") {
      i++;
      continue;
    }

    const para: string[] = [];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !lines[i].startsWith("```") &&
      !/^(#{1,4})\s/.test(lines[i]) &&
      !/^\s*([-*]|\d+\.)\s+/.test(lines[i]) &&
      !isTableRow(lines[i])
    )
      para.push(lines[i++]);
    if (para.length === 0) para.push(lines[i++]);
    blocks.push(<p key={key}>{inline(para.join(" "))}</p>);
  }
  return <div className="flex flex-col gap-3 text-[14px] leading-relaxed text-ink">{blocks}</div>;
}
