package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/0x2alx/gigg/internal/access"
	"github.com/0x2alx/gigg/internal/media"
	"github.com/0x2alx/gigg/internal/store"
)

// Attachments: files, links and commits on a gig. Seeing them = seeing the gig; attaching = Work on
// gigs; removing = your own (Work on gigs) or anyone's (Edit gigs). Keys need attachments:write to
// change them. A gig in the trash hides its attachments until it is restored.

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
		_, att, _, err := a.attachment(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		return &attachmentOut{Body: att}, nil
	})

	huma.Register(a.h, huma.Operation{OperationID: "get-attachment-file", Method: "GET", Path: "/attachments/{id}/file", Tags: []string{"attachments"},
		Summary: "An attached file's bytes: images and text in the page, everything else as a download"},
		func(ctx context.Context, in *attachFileIn) (*huma.StreamResponse, error) {
			_, att, _, err := a.attachment(ctx, in.ID)
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
		_, att, _, err := a.attachment(ctx, in.ID)
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
		Summary: "Remove an attachment (yours, or any with Edit gigs)", DefaultStatus: 204}, func(ctx context.Context, in *idPath) (*struct{}, error) {
		return nil, a.detach(ctx, in.ID)
	})
}

// attachment resolves an attachment on a live gig the caller sees.
func (a *API) attachment(ctx context.Context, id int64) (*access.Principal, *store.Attachment, *store.Gig, error) {
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
	if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Viewer, "gigs:read"); err != nil {
		return nil, nil, nil, err
	}
	return p, att, t, nil
}

// attachFile checks and stores an uploaded file, makes an image's preview, and attaches it.
func (a *API) attachFile(ctx context.Context, ref, filename, label string, run *int64, data []byte) (*store.Attachment, error) {
	if len(data) == 0 {
		return nil, fail(422, "invalid", "The file is empty.")
	}
	if store.CleanFilename(filename) == "" {
		return nil, fail(422, "invalid", "A file name is needed.")
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
	return a.attachWith(ctx, ref, in, func(in *store.AttachmentIn) error {
		// written inside the transaction, so the blob sweep (one writer) never removes a file under us
		var err error
		if in.Blob, err = a.st.WriteBlob(data); err != nil {
			return err
		}
		if len(thumb) > 0 {
			if in.Thumb, err = a.st.WriteBlob(thumb); err != nil {
				return err
			}
			in.ThumbLen = int64(len(thumb))
		}
		return nil
	})
}

// attach adds a link or commit (or a file already prepared by attachWith's callback).
func (a *API) attach(ctx context.Context, ref string, in store.AttachmentIn) (*store.Attachment, error) {
	return a.attachWith(ctx, ref, in, nil)
}

func (a *API) attachWith(ctx context.Context, ref string, in store.AttachmentIn, prep func(*store.AttachmentIn) error) (*store.Attachment, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	t, err := a.gig(ctx, p, ref)
	if err != nil {
		return nil, err
	}
	if t.Deleted {
		return nil, trashErr(t)
	}
	if err := requireIn(p, t.WorkspaceID, t.ProjectID, access.Worker, "attachments:write"); err != nil {
		return nil, err
	}
	var att *store.Attachment
	err = a.tx(ctx, func(tx *sql.Tx) error {
		cur, err := store.GetGig(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		if prep != nil {
			if err := prep(&in); err != nil {
				return err
			}
		}
		att, err = store.AddAttachment(ctx, tx, p.Actor(), cur, in)
		return err
	})
	return att, err
}

// detach removes an attachment: your own with Work on gigs, anyone's with Edit gigs.
func (a *API) detach(ctx context.Context, id int64) error {
	p, att, t, err := a.attachment(ctx, id)
	if err != nil {
		return err
	}
	perm := access.Worker
	if att.CreatedBy != p.User.ID {
		perm = access.Editor
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
		tag := `"` + sha[:16] + `"`
		hc.SetHeader("Cache-Control", "private, max-age=86400")
		hc.SetHeader("ETag", tag)
		if hc.Header("If-None-Match") == tag {
			hc.SetStatus(http.StatusNotModified)
			return
		}
		hc.SetHeader("Content-Type", ctype)
		hc.SetHeader("Content-Length", strconv.FormatInt(st.Size(), 10))
		hc.SetHeader("Content-Disposition", disposition)
		hc.SetHeader("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
		hc.SetStatus(http.StatusOK)
		_, _ = io.Copy(hc.BodyWriter(), f)
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
