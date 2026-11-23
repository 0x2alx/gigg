package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/media"
	"github.com/0x2alx/gigg/internal/store"
)

// Attachments: files, links and commits on a gig. Listing them = seeing the gig; a file's bytes =
// Download attachments (keys: attachments:read); attaching and removing your own = Add attachments,
// anyone's = Delete attachments (keys: attachments:write). A gig in the trash hides its attachments
// until it is restored.

type attachmentsOut struct {
	Body struct {
		Items   []*store.Attachment `json:"items"`
		FileMax int64               `json:"file_max" doc:"The largest file this workspace takes, in bytes"`
		Quota   int64               `json:"quota" doc:"The workspace's total for attachments, in bytes"`
		Used    int64               `json:"used" doc:"Bytes the workspace's attachments use"`
	}
}

type attachmentOut struct {
	Body *store.Attachment
}

type attachUploadIn struct {
	ID          string `path:"id" doc:"Gig id or key"`
	Filename    string `query:"filename" required:"true" maxLength:"1024" doc:"The file's name (the last path element is kept)"`
	Label       string `query:"label" maxLength:"200" doc:"A short caption"`
	RunID       int64  `query:"run_id" doc:"The run this file comes from (runners)"`
	ContentType string `header:"Content-Type"`
	RawBody     []byte `contentType:"application/octet-stream"`
}

type attachRefIn struct {
	ID   string `path:"id" doc:"Gig id or key"`
	Body struct {
		Kind  string `json:"kind" enum:"link,commit" doc:"link (an http(s) URL) or commit (a commit id)"`
		Value string `json:"value" maxLength:"2048" doc:"The URL or the commit id"`
		Label string `json:"label,omitempty" maxLength:"200" doc:"A short caption"`
		RunID *int64 `json:"run_id,omitempty" doc:"The run it comes from (runners)"`
	}
}

type attachFileIn struct {
	ID       int64 `path:"id"`
	Download bool  `query:"download" doc:"Always as a download, even an image or text"`
}

func (a *API) registerAttachments() {
	a.registerAttachLimits()
	huma.Register(a.h, huma.Operation{OperationID: "list-attachments", Method: "GET", Path: "/gigs/{id}/attachments", Tags: []string{"attachments"},
		Summary: "A gig's attachments (oldest first) and the workspace's limits"}, func(ctx context.Context, in *gigPath) (*attachmentsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		t, err := a.gig(ctx, p, in.ID)
		if err != nil {
			return nil, err
		}
		if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Viewer, "gigs:read"); err != nil {
			return nil, err
		}
		out := &attachmentsOut{}
		out.Body.Items = []*store.Attachment{}
		if !t.Deleted {
			if out.Body.Items, err = store.Attachments(ctx, a.st.R, t.ID); err != nil {
				return nil, err
			}
		}
		out.Body.FileMax, out.Body.Quota, out.Body.Used, err = store.AttachLimits(ctx, a.st.R, t.WorkspaceID)
		return out, storeErr(err)
	})

	huma.Register(a.h, huma.Operation{OperationID: "upload-attachment", Method: "POST", Path: "/gigs/{id}/attachments", Tags: []string{"attachments"},
		Summary:       "Attach a file (the request body is the file; up to the workspace's limit, 10 MB by default)",
		DefaultStatus: 201, MaxBodyBytes: store.AttachFileMaxCeiling + 1024}, func(ctx context.Context, in *attachUploadIn) (*attachmentOut, error) {
		var run *int64
		if in.RunID != 0 {
			run = &in.RunID
		}
		att, err := a.attachFile(ctx, in.ID, in.Filename, in.Label, run, in.RawBody)
		if err != nil {
			return nil, err
		}
		return &attachmentOut{Body: att}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "attach-link", Method: "POST", Path: "/gigs/{id}/attachments/link", Tags: []string{"attachments"},
		Summary: "Attach a link or a commit", DefaultStatus: 201}, func(ctx context.Context, in *attachRefIn) (*attachmentOut, error) {
		att, err := a.attach(ctx, in.ID, store.AttachmentIn{Kind: in.Body.Kind, Value: in.Body.Value, Label: in.Body.Label, RunID: in.Body.RunID})
		if err != nil {
			return nil, err
		}
		return &attachmentOut{Body: att}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "get-attachment", Method: "GET", Path: "/attachments/{id}", Tags: []string{"attachments"},
		Summary: "One attachment"}, func(ctx context.Context, in *idPath) (*attachmentOut, error) {
		_, att, _, err := a.attachment(ctx, in.ID, access.Viewer, "gigs:read")
		if err != nil {
			return nil, err
		}
		return &attachmentOut{Body: att}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "get-attachment-file", Method: "GET", Path: "/attachments/{id}/file", Tags: []string{"attachments"},
		Summary: "An attached file's bytes: images and text in the page, everything else as a download"},
		func(ctx context.Context, in *attachFileIn) (*huma.StreamResponse, error) {
			_, att, _, err := a.attachment(ctx, in.ID, "download_attachments", "attachments:read")
			if err != nil {
				return nil, err
			}
			blob, _ := att.Blob()
			if blob == "" {
				return nil, errNotFound()
			}
			ctype, disp := "application/octet-stream", "attachment"
			if att.Inline && !in.Download {
				ctype, disp = *att.Mime, "inline"
				if ctype == "text/markdown" || ctype == "text/plain" {
					ctype = "text/plain; charset=utf-8" // the page renders Markdown itself
				}
			}
			d := mime.FormatMediaType(disp, map[string]string{"filename": *att.Filename})
			if d == "" {
				d = disp
			}
			return a.serveBlob(blob, ctype, d)
		})

	huma.Register(a.h, huma.Operation{OperationID: "get-attachment-thumb", Method: "GET", Path: "/attachments/{id}/thumb", Tags: []string{"attachments"},
		Summary: "An image's preview (at most 480 px a side)"}, func(ctx context.Context, in *idPath) (*huma.StreamResponse, error) {
		_, att, _, err := a.attachment(ctx, in.ID, "download_attachments", "attachments:read")
		if err != nil {
			return nil, err
		}
		_, thumb := att.Blob()
		if thumb == "" {
			return nil, errNotFound()
		}
		return a.serveBlob(thumb, "", "inline")
	})

	huma.Register(a.h, huma.Operation{OperationID: "delete-attachment", Method: "DELETE", Path: "/attachments/{id}", Tags: []string{"attachments"},
		Summary: "Remove an attachment (yours, or any with Delete attachments)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		return nil, a.detach(ctx, in.ID)
	})
}

// attachment resolves an attachment on a live gig the caller sees, and checks perm and scope there.
func (a *API) attachment(ctx context.Context, id int64, perm, scope string) (*access.Principal, *store.Attachment, *store.Gig, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	att, err := store.GetAttachment(ctx, a.st.R, id)
	if err != nil {
		return nil, nil, nil, errNotFound()
	}
	t, err := store.GetGig(ctx, a.st.R, att.GigID)
	if err != nil || t.Deleted || !p.SeesProject(t.WorkspaceID, t.ProjectID) {
		return nil, nil, nil, errNotFound()
	}
	if err := requireIn(p, t.WorkspaceID, t.ProjectID, perm, scope); err != nil {
		return nil, nil, nil, err
	}
	return p, att, t, nil
}

// attachFile checks and stores an uploaded file, makes an image's preview, and attaches it. The caller's
// rights and the workspace's limits are checked before any decoding or disk write.
func (a *API) attachFile(ctx context.Context, ref, filename, label string, run *int64, data []byte) (*store.Attachment, error) {
	if len(data) == 0 {
		return nil, fail(422, "invalid", "The file is empty.")
	}
	if store.CleanFilename(filename) == "" {
		return nil, fail(422, "invalid", "A file name is needed.")
	}
	p, t, err := a.attachTarget(ctx, ref)
	if err != nil {
		return nil, err
	}
	fileMax, quota, used, err := store.AttachLimits(ctx, a.st.R, t.WorkspaceID)
	if err == nil {
		err = store.FitsLimits(fileMax, quota, used, int64(len(data)))
	}
	if err != nil {
		return nil, storeErr(err) // checked again in the transaction, against a concurrent upload
	}
	info := media.Sniff(data, filename)
	in := store.AttachmentIn{Kind: "file", Label: label, RunID: run, Size: int64(len(data)), Filename: filename,
		Mime: info.Mime, Width: int64(info.Width), Height: int64(info.Height)}
	var thumb []byte
	if info.Width > 0 {
		// decoding can take ~64 MiB: share the two heavy-work slots with login and pictures
		select {
		case a.hashSlots <- struct{}{}:
			thumb, _, _ = media.Thumb(data, info) // no preview is fine: the tile shows the file instead
			<-a.hashSlots
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return a.attachWith(ctx, p, t, in, func(in *store.AttachmentIn, made *[]string) error {
		// written inside the transaction, so the blob sweep (one writer) never removes a file under us;
		// made lists the files this upload created, for attachWith to remove if the attachment fails
		sha, created, err := a.st.WriteBlobNew(data)
		if err != nil {
			return err
		}
		in.Blob = sha
		if created {
			*made = append(*made, sha)
		}
		if len(thumb) > 0 {
			if sha, created, err = a.st.WriteBlobNew(thumb); err != nil {
				return err
			}
			in.Thumb, in.ThumbLen = sha, int64(len(thumb))
			if created {
				*made = append(*made, sha)
			}
		}
		return nil
	})
}

// attach adds a link or commit.
func (a *API) attach(ctx context.Context, ref string, in store.AttachmentIn) (*store.Attachment, error) {
	p, t, err := a.attachTarget(ctx, ref)
	if err != nil {
		return nil, err
	}
	return a.attachWith(ctx, p, t, in, nil)
}

// attachTarget: the caller and a live gig they may attach to (Add attachments, attachments:write).
func (a *API) attachTarget(ctx context.Context, ref string) (*access.Principal, *store.Gig, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, nil, err
	}
	t, err := a.gig(ctx, p, ref)
	if err != nil {
		return nil, nil, err
	}
	if t.Deleted {
		return nil, nil, trashErr(t)
	}
	if err := requireIn(p, t.WorkspaceID, t.ProjectID, "add_attachments", "attachments:write"); err != nil {
		return nil, nil, err
	}
	return p, t, nil
}

// attachWith adds the attachment in one write transaction; prep (files) writes the blobs there first
// and lists the ones it created, which are removed again if the attachment doesn't make it.
func (a *API) attachWith(ctx context.Context, p *access.Principal, t *store.Gig, in store.AttachmentIn, prep func(*store.AttachmentIn, *[]string) error) (*store.Attachment, error) {
	var att *store.Attachment
	var made []string
	undone := false
	err := a.tx(ctx, func(tx *sql.Tx) (err error) {
		made, undone = nil, false // per attempt, should the transaction ever be retried
		defer func() {
			if err != nil {
				for _, sha := range made {
					a.st.RemoveBlob(sha) // still inside the transaction: no other writer can be using the new files
				}
				undone = true
			}
		}()
		cur, err := store.GetGig(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if prep != nil {
			if err = prep(&in, &made); err != nil {
				return err
			}
		}
		att, err = store.AddAttachment(ctx, tx, p.Actor(), cur, in)
		return err
	})
	if err != nil && !undone && len(made) > 0 {
		// the commit itself failed: no row points at the new files. Remove them under the writer again,
		// unless an upload of the same bytes has started using them meanwhile (the sweep would get them later anyway)
		bg := context.WithoutCancel(ctx) // a cancelled request still cleans up
		_ = a.st.Tx(bg, func(tx *sql.Tx) error {
			for _, sha := range made {
				a.st.RemoveUnusedBlob(bg, tx, sha)
			}
			return nil
		})
	}
	return att, err
}

// detach removes an attachment: your own with Add attachments, anyone's with Delete attachments.
func (a *API) detach(ctx context.Context, id int64) error {
	p, att, t, err := a.attachment(ctx, id, access.Viewer, "gigs:read")
	if err != nil {
		return err
	}
	perm := "add_attachments"
	if att.CreatedBy != p.User.ID {
		perm = "delete_attachments"
	}
	if err := requireIn(p, t.WorkspaceID, t.ProjectID, perm, "attachments:write"); err != nil {
		return err
	}
	return a.tx(ctx, func(tx *sql.Tx) error { return store.DeleteAttachment(ctx, tx, p.Actor(), t, att) })
}

// serveBlob streams a stored file. ctype "" = sniff (thumbnails are JPEG or PNG). The sandbox CSP
// keeps a file opened on its own from running anything, whatever it turns out to be.
func (a *API) serveBlob(sha, ctype, disposition string) (*huma.StreamResponse, error) {
	f, err := os.Open(a.st.BlobPath(sha))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNotFound()
		}
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if ctype == "" {
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		ctype = http.DetectContentType(head[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &huma.StreamResponse{Body: func(hc huma.Context) {
		defer f.Close()
		r, w := humago.Unwrap(hc)
		h := w.Header()
		h.Set("Cache-Control", "private, no-cache") // revalidated (ETag, 304) each time: a revoked right stops it
		h.Set("ETag", `"`+sha[:16]+`"`)
		h.Set("Content-Type", ctype)
		h.Set("Content-Disposition", disposition)
		h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'; sandbox")
		// ranges (video and audio seek), If-None-Match and HEAD, by the standard library
		http.ServeContent(w, r, "", st.ModTime(), f)
	}}, nil
}

type attachLimitsIn struct {
	WS   string `path:"ws" doc:"Workspace id, slug, or current"`
	Body struct {
		FileMax *int64 `json:"file_max" doc:"The largest file in bytes (null = the default, 10 MB; at most 50 MB)"`
		Quota   *int64 `json:"quota" doc:"The total for all attachments in bytes (null = the default, 1 GB)"`
	}
}

type attachLimitsOut struct {
	Body struct {
		FileMax int64 `json:"file_max"`
		Quota   int64 `json:"quota"`
		Used    int64 `json:"used"`
		Custom  bool  `json:"custom" doc:"Either limit differs from the default"`
	}
}

func (a *API) registerAttachLimits() {
	huma.Register(a.h, huma.Operation{OperationID: "get-attachment-limits", Method: "GET", Path: "/workspaces/{ws}/attachment-limits", Tags: []string{"attachments"},
		Summary: "A workspace's attachment limits and use"}, func(ctx context.Context, in *wsPath) (*attachLimitsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := require(p, w.ID, access.Viewer, "workspace:read"); err != nil {
			return nil, err
		}
		return a.attachLimits(ctx, w.ID)
	})
	huma.Register(a.h, huma.Operation{OperationID: "set-attachment-limits", Method: "PUT", Path: "/workspaces/{ws}/attachment-limits", Tags: []string{"attachments"},
		Summary: "Set a workspace's attachment limits (admins)"}, func(ctx context.Context, in *attachLimitsIn) (*attachLimitsOut, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		if err := adminWith(p, "admin:workspaces"); err != nil {
			return nil, err
		}
		w, err := a.workspace(ctx, p, in.WS)
		if err != nil {
			return nil, err
		}
		if err := a.tx(ctx, func(tx *sql.Tx) error {
			return store.SetAttachLimits(ctx, tx, p.Actor(), w.ID, in.Body.FileMax, in.Body.Quota)
		}); err != nil {
			return nil, err
		}
		return a.attachLimits(ctx, w.ID)
	})
}

func (a *API) attachLimits(ctx context.Context, ws int64) (*attachLimitsOut, error) {
	out := &attachLimitsOut{}
	var err error
	out.Body.FileMax, out.Body.Quota, out.Body.Used, err = store.AttachLimits(ctx, a.st.R, ws)
	out.Body.Custom = out.Body.FileMax != store.DefaultAttachFileMax || out.Body.Quota != store.DefaultAttachQuota
	return out, storeErr(err)
}
