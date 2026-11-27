// The gig panel's Attachments: images as thumbnail tiles (the lightbox shows the file), Markdown and
// text as rows that open a viewer, other files as rows with a download, links and commits as chips.
// Attaching: the Attach button (several files, sent one at a time), files dropped on the panel, and
// Add link (a URL or a commit id). Reloaded when a live event names an attachment of this gig.
import { useCallback, useEffect, useRef, useState, type FormEvent, type RefObject } from "react";
import { api, fmtTime, type Attachment, type AttachmentList, type Gig } from "./api";
import { useApp } from "./App";
import { attachmentName, canRemove, fileURL, fmtSize, groupAttachments, refKind, shortCommit, shownAs, thumbURL, tileSrc, type AttachmentGroups } from "./attachments";
import { errText, Section } from "./GigSections";
import { Icon } from "./icons";
import { onActivity } from "./live";
import { Lightbox, type ViewItem } from "./Lightbox";
import { Markdown } from "./markdown";
import { ConfirmModal } from "./ui";

interface Busy {
  at: number; // 1-based
  of: number;
  name: string;
}

export function AttachmentsSection({ gig, canAdd, canDeleteAny, canDownload, dropZone, onChanged }: {
  gig: Gig;
  canAdd: boolean; // may attach and remove their own (Add attachments, and the gig isn't in the trash)
  canDeleteAny: boolean; // may remove anyone's (Delete attachments)
  canDownload: boolean; // may open files (Download attachments); without it, names only
  dropZone?: RefObject<HTMLElement | null>; // files dropped here are attached (the panel)
  onChanged?: () => void;
}) {
  const { me, users } = useApp();
  const [list, setList] = useState<AttachmentList | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState<Busy | null>(null);
  const [viewing, setViewing] = useState<number | null>(null); // the attachment the viewer opened on
  const [removing, setRemoving] = useState<Attachment | null>(null);
  const [linking, setLinking] = useState(false);
  const picker = useRef<HTMLInputElement>(null);
  const gigId = useRef(gig.id);
  gigId.current = gig.id;

  const load = useCallback(() => {
    const id = gig.id;
    api.attachments(id).then(
      (r) => gigId.current === id && setList(r),
      (e: unknown) => gigId.current === id && setErr(errText(e, "Couldn't load the attachments.")),
    );
  }, [gig.id]);
  useEffect(() => {
    setList(null);
    setErr("");
    load();
  }, [load]);

  // live: an attachment added to or removed from this gig (by anyone) reloads the list
  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = onActivity(({ item }) => {
      if (!item.action.startsWith("attachment.")) return;
      if ((item.data as { gig_id?: unknown } | null)?.gig_id !== gig.id) return;
      clearTimeout(timer);
      timer = setTimeout(load, 400);
    });
    return () => {
      off();
      clearTimeout(timer);
    };
  }, [load, gig.id]);

  // upload: the files one at a time; a refused one (too large, the workspace full) is reported and
  // the rest still go
  const sending = useRef(false);
  const upload = useCallback(
    async (files: File[]) => {
      if (!files.length || sending.current) return;
      sending.current = true;
      setErr("");
      const id = gig.id;
      const problems: string[] = [];
      for (let i = 0; i < files.length; i++) {
        const f = files[i]!;
        setBusy({ at: i + 1, of: files.length, name: f.name });
        if (list && list.file_max > 0 && f.size > list.file_max) {
          problems.push(`${f.name}: too large (${fmtSize(f.size)}; the limit is ${fmtSize(list.file_max)}).`);
          continue;
        }
        try {
          await api.uploadAttachment(id, f, f.name);
        } catch (e) {
          problems.push(`${f.name}: ${errText(e, "Couldn't attach it.")}`);
        }
      }
      sending.current = false;
      setBusy(null);
      if (gigId.current !== id) return;
      setErr(problems.join(" "));
      load();
      onChanged?.();
    },
    [gig.id, list, load, onChanged],
  );
  const uploadRef = useRef(upload);
  uploadRef.current = upload;

  // files dragged over the panel: it shows as a drop target; dropped, they are attached
  useEffect(() => {
    const zone = dropZone?.current;
    if (!zone || !canAdd) return;
    let depth = 0;
    const hasFiles = (e: DragEvent) => !!e.dataTransfer && [...e.dataTransfer.types].includes("Files");
    const set = (on: boolean) => zone.classList.toggle("att-drop", on);
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth++;
      set(true);
    };
    const over = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      e.dataTransfer!.dropEffect = "copy";
    };
    const leave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) set(false);
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      set(false);
      void uploadRef.current([...(e.dataTransfer?.files ?? [])]);
    };
    zone.addEventListener("dragenter", enter);
    zone.addEventListener("dragover", over);
    zone.addEventListener("dragleave", leave);
    zone.addEventListener("drop", drop);
    // a drop an editor takes (it stops it there, so it is not attached twice) still ends the highlight
    const reset = () => {
      depth = 0;
      set(false);
    };
    zone.addEventListener("drop", reset, true);
    return () => {
      zone.removeEventListener("drop", reset, true);
      set(false);
      zone.removeEventListener("dragenter", enter);
      zone.removeEventListener("dragover", over);
      zone.removeEventListener("dragleave", leave);
      zone.removeEventListener("drop", drop);
    };
  }, [dropZone, canAdd, gig.id]);

  const remove = (a: Attachment) => {
    setRemoving(null);
    setErr("");
    api.deleteAttachment(a.id).then(
      () => {
        load();
        onChanged?.();
      },
      (e: unknown) => setErr(errText(e, "Couldn't remove it.")),
    );
  };

  const items = list?.items ?? [];
  if (!canAdd && items.length === 0) return null; // nothing to see and nothing to do
  const g = groupAttachments(items, canDownload);
  const views = viewing !== null ? viewItems(g, canDownload) : [];
  const removable = (a: Attachment) => canRemove(a, me.user.id, canAdd, canDeleteAny);
  const who = (a: Attachment) => {
    const u = users.get(a.created_by);
    return u?.display_name || u?.username || a.created_by_username;
  };
  const del = (a: Attachment) =>
    removable(a) ? (
      <button type="button" className="icon-button att-del" onClick={() => setRemoving(a)} aria-label={`Remove ${attachmentName(a)}`} title="Remove">
        <Icon name="trash" />
      </button>
    ) : null;

  return (
    <Section label="Attachments" title="Attachments" icon="paperclip" badges={items.length > 0 && <span className="tsec-count">{items.length}</span>}>
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
      {list === null && !err && <p className="muted small">Loading…</p>}

      {g.images.length > 0 && (
        <ul className="att-grid" aria-label="Images">
          {g.images.map((a) => (
            <li key={a.id} className="att-tile">
              <button type="button" className="att-thumb" onClick={() => setViewing(a.id)} aria-label={`Open ${attachmentName(a)}`} title={`${a.filename ?? ""} · ${fmtSize(a.size)} · ${who(a)}, ${fmtTime(a.created_at)}`}>
                <img src={tileSrc(a)} alt="" loading="lazy" />
              </button>
              <span className="att-cap" title={attachmentName(a)}>
                {attachmentName(a)}
              </span>
              {del(a)}
            </li>
          ))}
        </ul>
      )}

      {g.media.length > 0 && (
        <ul className="att-list att-media" aria-label="Video and audio">
          {g.media.map((a) => (
            <li key={a.id}>
              <div className="att-media-head">
                <Icon name="file" className="att-ico" />
                <span className="att-main">
                  <span className="att-name">{attachmentName(a)}</span>
                  <span className="muted small att-meta">
                    {a.label && a.filename ? `${a.filename} · ` : ""}
                    {fmtSize(a.size)} · {who(a)} · {fmtTime(a.created_at)}
                  </span>
                </span>
                <a className="icon-button att-dl" href={fileURL(a.id, true)} download={a.filename ?? true} aria-label={`Download ${a.filename ?? "the file"}`} title="Download">
                  <Icon name="download" />
                </a>
                {del(a)}
              </div>
              {shownAs(a) === "video" ? (
                <video className="att-video" src={fileURL(a.id)} controls preload="metadata" aria-label={attachmentName(a)} />
              ) : (
                <audio className="att-audio" src={fileURL(a.id)} controls preload="metadata" aria-label={attachmentName(a)} />
              )}
            </li>
          ))}
        </ul>
      )}

      {g.docs.length > 0 && (
        <ul className="att-list" aria-label="Files">
          {g.docs.map((a) => {
            const s = shownAs(a);
            const viewable = s === "markdown" || s === "text";
            return (
              <li key={a.id}>
                {a.has_thumb && canDownload ? (
                  // a preview of a file the page doesn't show (TIFF, HEIC): clicking downloads it
                  <a className="att-rowthumb" href={fileURL(a.id, true)} download={a.filename ?? true} tabIndex={-1} aria-hidden="true">
                    <img src={thumbURL(a.id)} alt="" loading="lazy" />
                  </a>
                ) : (
                  <Icon name="file" className="att-ico" />
                )}
                <span className="att-main">
                  {!canDownload ? (
                    <span className="att-name">{attachmentName(a)}</span>
                  ) : viewable ? (
                    <button type="button" className="link att-name" onClick={() => setViewing(a.id)}>
                      {attachmentName(a)}
                    </button>
                  ) : (
                    <a className="att-name" href={fileURL(a.id, true)} download={a.filename ?? true}>
                      {attachmentName(a)}
                    </a>
                  )}
                  <span className="muted small att-meta">
                    {a.label && a.filename ? `${a.filename} · ` : ""}
                    {fmtSize(a.size)} · {who(a)} · {fmtTime(a.created_at)}
                  </span>
                </span>
                {canDownload && (
                  <a className="icon-button att-dl" href={fileURL(a.id, true)} download={a.filename ?? true} aria-label={`Download ${a.filename ?? "the file"}`} title="Download">
                    <Icon name="download" />
                  </a>
                )}
                {del(a)}
              </li>
            );
          })}
        </ul>
      )}

      {g.refs.length > 0 && (
        <ul className="att-refs" aria-label="Links and commits">
          {g.refs.map((a) => (
            <li key={a.id} className="att-chip" title={`${a.value} · ${who(a)}, ${fmtTime(a.created_at)}`}>
              {a.kind === "link" ? (
                <a href={a.value} target="_blank" rel="noopener noreferrer">
                  <Icon name="link" />
                  <span className="att-chip-text">{a.label || a.value.replace(/^https?:\/\//, "")}</span>
                </a>
              ) : (
                <span className="att-commit">
                  <Icon name="commit" />
                  <code>{shortCommit(a.value)}</code>
                  {a.label && <span className="att-chip-text">{a.label}</span>}
                </span>
              )}
              {del(a)}
            </li>
          ))}
        </ul>
      )}

      {canAdd && (
        <>
          <div className="att-actions">
            <button type="button" className="button secondary small" onClick={() => picker.current?.click()} disabled={!!busy}>
              <Icon name="paperclip" /> Attach
            </button>
            <input
              ref={picker}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                const files = [...(e.target.files ?? [])];
                e.target.value = ""; // the same file can be picked again
                void upload(files);
              }}
            />
            {!linking && (
              <button type="button" className="button secondary small" onClick={() => setLinking(true)}>
                <Icon name="link" /> Add link
              </button>
            )}
            {busy ? (
              <span className="small att-busy" role="status">
                <span className="att-spin" aria-hidden="true" />
                Uploading {busy.of > 1 ? `${busy.at} of ${busy.of}: ` : ""}
                {busy.name}…
              </span>
            ) : (
              // the quota is the workspace's, not this gig's: say so, or it reads as what this gig holds
              list &&
              list.quota > 0 && (
                <span className="muted small att-usage" data-tip="Every gig's attachments in this workspace count toward its storage">
                  Workspace: {fmtSize(list.used)} of {fmtSize(list.quota)}
                </span>
              )
            )}
          </div>
          {items.length === 0 && !busy && !linking && <p className="muted small att-hint">Drop files on the panel to attach them.</p>}
          {linking && (
            <AddRef
              onCancel={() => setLinking(false)}
              onAdd={(value, label) =>
                api.attachRef(gig.id, refKind(value), value, label).then(
                  () => {
                    setLinking(false);
                    setErr("");
                    load();
                    onChanged?.();
                  },
                  (e: unknown) => {
                    throw new Error(errText(e, "Couldn't add it."));
                  },
                )
              }
            />
          )}
        </>
      )}

      {viewing !== null && <Lightbox items={views} start={Math.max(0, views.findIndex((x) => x.key === String(viewing)))} onClose={() => setViewing(null)} />}
      {removing && (
        <ConfirmModal title="Remove the attachment?" confirmLabel="Remove" onConfirm={() => remove(removing)} onCancel={() => setRemoving(null)}>
          <p>
            “{attachmentName(removing)}” is removed from {gig.key} for everyone.
          </p>
        </ConfirmModal>
      )}
    </Section>
  );
}

// AddRef: a URL or a commit id (7 to 64 hex digits) with an optional label.
function AddRef({ onAdd, onCancel }: { onAdd: (value: string, label: string) => Promise<void>; onCancel: () => void }) {
  const [value, setValue] = useState("");
  const [label, setLabel] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const v = value.trim();
  const kind = v ? refKind(v) : null;
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!v || busy) return;
    setBusy(true);
    setErr("");
    onAdd(v, label.trim())
      .catch((e: unknown) => setErr(e instanceof Error ? e.message : "Couldn't add it."))
      .finally(() => setBusy(false));
  };
  return (
    <form className="att-addref" onSubmit={submit} aria-label="Add link">
      <input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder="https://… or a commit id"
        aria-label="URL or commit id"
        maxLength={2048}
        autoFocus
        required
        data-dirty={v ? "" : undefined}
        onKeyDown={(e) => {
          if (e.key === "Escape" && !v) {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
      />
      <input value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Label (optional)" aria-label="Label" maxLength={200} />
      <div className="att-addref-foot">
        <span className="muted small">{kind === "commit" ? "A commit" : kind === "link" ? "A link" : ""}</span>
        <button type="button" className="button secondary small" onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className="button small" disabled={!v || busy}>
          Add
        </button>
      </div>
      {err && (
        <p className="danger small" role="alert">
          {err}
        </p>
      )}
    </form>
  );
}

// viewItems: what the viewer steps through, in the section's order: the images, then the Markdown and
// text files (the rows that open the viewer). Other files only download, so they are not in it.
function viewItems(g: AttachmentGroups, canDownload: boolean): ViewItem[] {
  if (!canDownload) return [];
  const docs = g.docs.filter((a) => {
    const s = shownAs(a);
    return s === "markdown" || s === "text";
  });
  return [
    ...g.images.map((a) => ({ key: String(a.id), title: attachmentName(a), image: { src: fileURL(a.id), alt: a.label || a.filename || "Attached image" }, foot: <FileFoot a={a} /> })),
    ...docs.map((a) => ({ key: String(a.id), title: attachmentName(a), body: <TextBody a={a} />, foot: <FileFoot a={a} /> })),
  ];
}

// TextBody: a Markdown file rendered like the comments (same renderer and sanitiser), plain text as is.
function TextBody({ a }: { a: Attachment }) {
  const [text, setText] = useState<string | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let live = true;
    api.attachmentText(a.id).then(
      (t) => live && setText(t),
      (e: unknown) => live && setErr(errText(e, "Couldn't load the file.")),
    );
    return () => {
      live = false;
    };
  }, [a.id]);
  return (
    <div className="att-viewer">
      {err ? <p className="danger">{err}</p> : text === null ? <p className="muted">Loading…</p> : shownAs(a) === "markdown" ? <Markdown source={text} /> : <pre className="att-pre">{text}</pre>}
    </div>
  );
}

function FileFoot({ a }: { a: Attachment }) {
  return (
    <div className="att-foot muted small">
      <span>
        {a.filename} · {fmtSize(a.size)}
        {a.width && a.height ? ` · ${a.width}×${a.height}` : ""}
      </span>
      <a className="button secondary small" href={fileURL(a.id, true)} download={a.filename ?? true}>
        <Icon name="download" /> Download
      </a>
    </div>
  );
}
