package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Attachments: files, links and commits on a gig. File bytes live in the blob store (WriteBlob);
// a row points at its blob and, for images, at a thumbnail blob.

// Attachment limits. A workspace may lower or raise them (an admin's call) within the ceilings.
const (
	DefaultAttachFileMax = 10 << 20 // 10 MiB per file
	DefaultAttachQuota   = 1 << 30  // 1 GiB per workspace
	AttachFileMaxCeiling = 50 << 20 // the upload endpoint reads at most this
	AttachQuotaCeiling   = 1 << 40
)

// ErrTooLarge (413): over the file size limit or the workspace's quota.
var ErrTooLarge = errors.New("too large")

func tooLarge(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrTooLarge, fmt.Sprintf(format, a...))
}

// Attachment is one file, link or commit on a gig.
type Attachment struct {
	ID                int64   `json:"id"`
	WorkspaceID       int64   `json:"workspace_id"`
	GigID             int64   `json:"gig_id"`
	RunID             *int64  `json:"run_id" doc:"The run that attached it, when a runner did"`
	Kind              string  `json:"kind" doc:"file, link or commit"`
	Label             string  `json:"label" doc:"A short caption ('' = none)"`
	Value             string  `json:"value" doc:"link: the URL; commit: the commit id; file: ''"`
	Filename          *string `json:"filename" doc:"Files: the name it was uploaded with"`
	Mime              *string `json:"mime" doc:"Files: the type the server found in the bytes (not the one claimed)"`
	Size              int64   `json:"size" doc:"Files: bytes"`
	Width             *int64  `json:"width" doc:"Images: pixels"`
	Height            *int64  `json:"height"`
	Inline            bool    `json:"inline" doc:"Shown in the page (images, Markdown, plain text); everything else is only downloaded"`
	HasThumb          bool    `json:"has_thumb" doc:"Images: a small preview exists at /attachments/{id}/thumb"`
	CreatedBy         int64   `json:"created_by"`
	CreatedByUsername string  `json:"created_by_username"`
	KeyID             *int64  `json:"key_id"`
	CreatedAt         int64   `json:"created_at"`

	blob, thumb string
}

// Blob returns the file's blob id and its thumbnail's ("" when none).
func (t *Attachment) Blob() (file, thumb string) { return t.blob, t.thumb }

// InlineMime reports the types gigg shows in the page; anything else is a download.
func InlineMime(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "text/markdown", "text/plain":
		return true
	}
	return false
}

const attachCols = `a.id, a.workspace_id, a.gig_id, a.run_id, a.kind, a.label, a.value, a.filename, a.mime, a.size,
	a.width, a.height, a.blob_sha256, a.thumb_sha256, a.created_by, u.username, a.key_id, a.created_at`

func scanAttachment(row interface{ Scan(...any) error }) (*Attachment, error) {
	var t Attachment
	var run, w, h, key sql.NullInt64
	var fn, mime, blob, thumb sql.NullString
	if err := row.Scan(&t.ID, &t.WorkspaceID, &t.GigID, &run, &t.Kind, &t.Label, &t.Value, &fn, &mime, &t.Size,
		&w, &h, &blob, &thumb, &t.CreatedBy, &t.CreatedByUsername, &key, &t.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	t.RunID, t.Width, t.Height, t.KeyID = ptrInt(run), ptrInt(w), ptrInt(h), ptrInt(key)
	t.Filename, t.Mime = ptrStr(fn), ptrStr(mime)
	t.blob, t.thumb = blob.String, thumb.String
	t.HasThumb = thumb.Valid
	t.Inline = t.Mime != nil && InlineMime(*t.Mime)
	return &t, nil
}

// AttachmentIn is a new attachment. Files arrive already stored (WriteBlob) and checked.
type AttachmentIn struct {
	Kind     string
	Label    string
	Value    string
	RunID    *int64
	Blob     string // file: sha256 of the stored bytes
	Size     int64
	Filename string
	Mime     string
	Width    int64
	Height   int64
	Thumb    string // images: sha256 of the stored thumbnail ("" = none)
	ThumbLen int64
}

var commitRe = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// CleanFilename keeps the last path element, drops control and format characters, and trims it to
// 255 bytes (on a character boundary). "" when nothing is left.
func CleanFilename(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Base("/" + s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if s == "/" || s == "." || s == ".." {
		return ""
	}
	for len(s) > 255 {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// AttachLimits returns a workspace's file size limit, its quota, and how much of it is used.
func AttachLimits(ctx context.Context, q Q, ws int64) (fileMax, quota, used int64, err error) {
	var fm, qu sql.NullInt64
	if err = q.QueryRowContext(ctx, `SELECT attach_file_max, attach_quota FROM workspaces WHERE id = ?`, ws).Scan(&fm, &qu); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return
	}
	fileMax, quota = DefaultAttachFileMax, DefaultAttachQuota
	if fm.Valid {
		fileMax = min(fm.Int64, AttachFileMaxCeiling)
	}
	if qu.Valid {
		quota = qu.Int64
	}
	err = q.QueryRowContext(ctx, `SELECT coalesce(sum(size), 0) FROM attachments WHERE workspace_id = ?`, ws).Scan(&used)
	return
}

// SetAttachLimits changes a workspace's limits (nil = back to the default).
func SetAttachLimits(ctx context.Context, q Q, a Actor, ws int64, fileMax, quota *int64) error {
	if fileMax != nil && (*fileMax < 1 || *fileMax > AttachFileMaxCeiling) {
		return Invalid("file size limit: 1 byte to %d MiB", AttachFileMaxCeiling>>20)
	}
	if quota != nil && (*quota < 1 || *quota > AttachQuotaCeiling) {
		return Invalid("attachment quota: 1 byte to %d TiB", AttachQuotaCeiling>>40)
	}
	res, err := q.ExecContext(ctx, `UPDATE workspaces SET attach_file_max = ?, attach_quota = ?, version = version + 1 WHERE id = ?`,
		nullInt(fileMax), nullInt(quota), ws)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = Activity(ctx, q, a, &ws, nil, "workspace.attach_limits", "workspace", ws, map[string]any{"file_max": fileMax, "quota": quota})
	return err
}

// AddAttachment attaches a file, link or commit to a live gig, within the workspace's limits.
func AddAttachment(ctx context.Context, q Q, a Actor, t *Gig, in AttachmentIn) (*Attachment, error) {
	if t.Deleted {
		return nil, Conflict("%s is in the trash: restore it first", t.Key)
	}
	label := strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(label) > 200 {
		return nil, Invalid("label: at most 200 characters")
	}
	if in.RunID != nil {
		r, err := GetRun(ctx, q, *in.RunID)
		if err != nil || r.GigID != t.ID {
			return nil, Invalid("run %d is not a run of this gig", *in.RunID)
		}
	}
	var blob, thumb, fn, mime any
	var w, h any
	value := strings.TrimSpace(in.Value)
	switch in.Kind {
	case "file":
		if in.Blob == "" || in.Size < 1 {
			return nil, Invalid("file: empty")
		}
		name := CleanFilename(in.Filename)
		if name == "" {
			return nil, Invalid("file: a file name is needed")
		}
		fileMax, quota, used, err := AttachLimits(ctx, q, t.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if in.Size > fileMax {
			return nil, tooLarge("files are up to %s in this workspace", HumanBytes(fileMax))
		}
		if used+in.Size > quota {
			return nil, tooLarge("the workspace's attachments are full (%s of %s used)", HumanBytes(used), HumanBytes(quota))
		}
		if err := refBlob(ctx, q, in.Blob, in.Size); err != nil {
			return nil, err
		}
		blob, fn, mime, value = in.Blob, name, in.Mime, ""
		if in.Thumb != "" {
			if err := refBlob(ctx, q, in.Thumb, in.ThumbLen); err != nil {
				return nil, err
			}
			thumb = in.Thumb
		}
		if in.Width > 0 && in.Height > 0 {
			w, h = in.Width, in.Height
		}
	case "link":
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(value) > 2048 {
			return nil, Invalid("link: an http(s) URL of at most 2048 characters")
		}
		in.Size = 0
	case "commit":
		if !commitRe.MatchString(value) {
			return nil, Invalid("commit: a commit id (7 to 64 hex digits)")
		}
		value = strings.ToLower(value)
		in.Size = 0
	default:
		return nil, Invalid("kind is file, link or commit")
	}
	res, err := q.ExecContext(ctx, `INSERT INTO attachments (workspace_id, gig_id, run_id, kind, label, value, blob_sha256,
		thumb_sha256, filename, mime, size, width, height, created_by, key_id, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.WorkspaceID, t.ID, nullInt(in.RunID), in.Kind, label, value, blob, thumb, fn, mime, in.Size, w, h, a.UserID, nullInt(a.KeyID), Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET updated_at = ? WHERE id = ?`, Now(), t.ID); err != nil {
		return nil, err
	}
	data := map[string]any{"gig_id": t.ID, "kind": in.Kind}
	if in.Kind == "file" {
		data["filename"], data["size"] = fn, in.Size
	} else {
		data["value"] = value
	}
	if _, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "attachment.created", "attachment", id, data); err != nil {
		return nil, err
	}
	return GetAttachment(ctx, q, id)
}

// refBlob records one more user of a stored file.
func refBlob(ctx context.Context, q Q, sha string, size int64) error {
	_, err := q.ExecContext(ctx, `INSERT INTO blobs (sha256, size, refcount) VALUES (?,?,1)
		ON CONFLICT (sha256) DO UPDATE SET refcount = refcount + 1, orphaned_at = NULL`, sha, size)
	return err
}

// GetAttachment loads one attachment.
func GetAttachment(ctx context.Context, q Q, id int64) (*Attachment, error) {
	return scanAttachment(q.QueryRowContext(ctx, "SELECT "+attachCols+` FROM attachments a JOIN users u ON u.id = a.created_by WHERE a.id = ?`, id))
}

// Attachments lists a gig's attachments, oldest first.
func Attachments(ctx context.Context, q Q, gig int64) ([]*Attachment, error) {
	rows, err := q.QueryContext(ctx, "SELECT "+attachCols+` FROM attachments a JOIN users u ON u.id = a.created_by
		WHERE a.gig_id = ? ORDER BY a.id`, gig)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Attachment{}
	for rows.Next() {
		t, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteAttachment removes an attachment. Its file stays on disk until the sweep finds it unused
// for longer than the snapshot window.
func DeleteAttachment(ctx context.Context, q Q, a Actor, t *Gig, att *Attachment) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM attachments WHERE id = ?`, att.ID); err != nil {
		return err
	}
	for _, sha := range []string{att.blob, att.thumb} {
		if sha != "" {
			if _, err := q.ExecContext(ctx, `UPDATE blobs SET refcount = max(refcount - 1, 0) WHERE sha256 = ?`, sha); err != nil {
				return err
			}
		}
	}
	if _, err := q.ExecContext(ctx, `UPDATE gigs SET updated_at = ? WHERE id = ?`, Now(), t.ID); err != nil {
		return err
	}
	data := map[string]any{"gig_id": t.ID, "kind": att.Kind}
	if att.Filename != nil {
		data["filename"] = *att.Filename
	} else {
		data["value"] = att.Value
	}
	_, err := Activity(ctx, q, a, &t.WorkspaceID, &t.ProjectID, "attachment.deleted", "attachment", att.ID, data)
	return err
}

// HumanBytes writes a size the way the limits are shown: 10 MB, 1 GB, 512 KB (binary units).
func HumanBytes(n int64) string {
	switch {
	case n >= 1<<40 && n%(1<<40) == 0:
		return fmt.Sprintf("%d TB", n>>40)
	case n >= 1<<30:
		return trimZero(float64(n)/(1<<30)) + " GB"
	case n >= 1<<20:
		return trimZero(float64(n)/(1<<20)) + " MB"
	case n >= 1<<10:
		return trimZero(float64(n)/(1<<10)) + " KB"
	}
	return fmt.Sprintf("%d bytes", n)
}

func trimZero(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	return strings.TrimSuffix(s, ".0")
}
