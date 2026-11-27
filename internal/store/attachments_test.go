package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func (f *fixture) attachFile(t *testing.T, g *Gig, data string) *Attachment {
	t.Helper()
	var att *Attachment
	f.tx(t, func(tx *sql.Tx) error {
		sha, err := f.s.WriteBlob([]byte(data))
		if err != nil {
			return err
		}
		att, err = AddAttachment(f.ctx, tx, f.a, g, AttachmentIn{Kind: "file", Blob: sha, Size: int64(len(data)), Filename: "a.txt", Mime: "text/plain"})
		return err
	})
	return att
}

// TestBlobSweep: a file stays while anything uses it, and for the grace period after; a file shared
// by two attachments outlives the first one's removal; cascades are caught by the recount; stray files
// with no row go once old.
func TestBlobSweep(t *testing.T) {
	f := newFixture(t)
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "with files"})
	a1 := f.attachFile(t, g, "same bytes")
	a2 := f.attachFile(t, g, "same bytes")
	sha, _ := a1.Blob()
	path := f.s.BlobPath(sha)
	exists := func() bool { _, err := os.Stat(path); return err == nil }

	f.tx(t, func(tx *sql.Tx) error { return DeleteAttachment(f.ctx, tx, f.a, g, a1) })
	if n, err := f.s.SweepBlobs(f.ctx, 0); err != nil || n != 0 || !exists() {
		t.Fatalf("still used by a2: removed %d, %v, exists %v", n, err, exists())
	}
	f.tx(t, func(tx *sql.Tx) error { return DeleteAttachment(f.ctx, tx, f.a, g, a2) })
	if n, _ := f.s.SweepBlobs(f.ctx, time.Hour); n != 0 || !exists() {
		t.Fatalf("within grace: removed %d", n)
	}
	// an hour later
	real := Now
	defer func() { Now = real }()
	Now = func() int64 { return real() + 2*time.Hour.Milliseconds() }
	if n, _ := f.s.SweepBlobs(f.ctx, time.Hour); n != 1 || exists() {
		t.Fatalf("after grace: removed %d, exists %v", n, exists())
	}
	Now = real

	// a cascade (the project goes) never decrements refcount; the recount notices
	a3 := f.attachFile(t, g, "cascaded")
	sha3, _ := a3.Blob()
	f.tx(t, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(f.ctx, `DELETE FROM attachments WHERE id = ?`, a3.ID)
		return err
	})
	if n, _ := f.s.SweepBlobs(f.ctx, 0); n != 1 {
		t.Fatalf("cascade: removed %d", n)
	}
	if _, err := os.Stat(f.s.BlobPath(sha3)); err == nil {
		t.Fatal("cascaded file kept")
	}

	// a stray file (no row): kept while new, removed once older than grace
	stray, err := f.s.WriteBlob([]byte("never committed"))
	must(t, err)
	if n, _ := f.s.SweepBlobs(f.ctx, time.Hour); n != 0 {
		t.Fatalf("new stray removed: %d", n)
	}
	old := time.Now().Add(-2 * time.Hour)
	must(t, os.Chtimes(f.s.BlobPath(stray), old, old))
	if n, _ := f.s.SweepBlobs(f.ctx, time.Hour); n != 1 {
		t.Fatalf("old stray: removed %d", n)
	}
	// files that aren't ours are left alone
	other := filepath.Join(filepath.Dir(f.s.BlobPath(stray)), "README")
	must(t, os.WriteFile(other, []byte("x"), 0o600))
	must(t, os.Chtimes(other, old, old))
	f.s.SweepBlobs(f.ctx, time.Hour)
	if _, err := os.Stat(other); err != nil {
		t.Fatal("foreign file removed")
	}
	// more strays than one write transaction looks at: all go, batch by batch; a stray that a row
	// points at by then (an upload reused it) stays
	n := sweepBatch*2 + 7
	var kept string
	for i := 0; i < n; i++ {
		sha, err := f.s.WriteBlob([]byte(fmt.Sprintf("stray %d", i)))
		must(t, err)
		must(t, os.Chtimes(f.s.BlobPath(sha), old, old))
		if i == n-1 {
			kept = sha
		}
	}
	_, err = f.s.W.ExecContext(f.ctx, `INSERT INTO blobs (sha256, size, refcount) VALUES (?, 1, 1)`, kept)
	must(t, err)
	if got, err := f.s.SweepBlobs(f.ctx, time.Hour); err != nil || got != n-1 {
		t.Fatalf("batched sweep: removed %d of %d (%v)", got, n-1, err)
	}
	if _, err := os.Stat(f.s.BlobPath(kept)); err != nil {
		t.Fatal("a stray a row points at was removed")
	}
}

// TestAttachmentQuota: the defaults apply until set; use is what the workspace's attachments take on
// disk: each stored file and image preview once, so a preview counts and a file attached twice
// doesn't count twice; the quota check counts the same way, preview included.
func TestAttachmentQuota(t *testing.T) {
	f := newFixture(t)
	g := f.gig(t, NewGig{ProjectID: f.p1.ID, Title: "q"})
	usedIs := func(want int64) {
		t.Helper()
		fm, q, used, err := AttachLimits(f.ctx, f.s.R, f.ws.ID)
		if err != nil || fm != DefaultAttachFileMax || used != want {
			t.Fatalf("limits: file max %d, quota %d, used %d (want %d), %v", fm, q, used, want, err)
		}
	}
	usedIs(0)
	if _, q, _, _ := AttachLimits(f.ctx, f.s.R, f.ws.ID); q != DefaultAttachQuota {
		t.Fatalf("default quota %d", q)
	}
	f.attachFile(t, g, "12345")
	usedIs(5)
	f.attachFile(t, g, "12345") // stored once
	usedIs(5)
	image := func(data, thumb string) error {
		return f.s.Tx(f.ctx, func(tx *sql.Tx) error {
			sha, _ := f.s.WriteBlob([]byte(data))
			th, _ := f.s.WriteBlob([]byte(thumb))
			_, err := AddAttachment(f.ctx, tx, f.a, g, AttachmentIn{Kind: "file", Blob: sha, Size: int64(len(data)), Filename: "i.png",
				Mime: "image/png", Width: 1, Height: 1, Thumb: th, ThumbLen: int64(len(thumb))})
			return err
		})
	}
	must(t, image("abc", "thumbbytes"))
	usedIs(5 + 3 + 10)

	quota := int64(20)
	f.tx(t, func(tx *sql.Tx) error { return SetAttachLimits(f.ctx, tx, f.a, f.ws.ID, nil, &quota) })
	// 2 bytes left: a 2-byte image fits, but not with its 8-byte preview
	if err := image("de", "preview!"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over quota with the preview: %v", err)
	}
	// the same image and preview again add nothing, so it fits even when full
	must(t, image("abc", "thumbbytes"))
	quota = 18
	f.tx(t, func(tx *sql.Tx) error { return SetAttachLimits(f.ctx, tx, f.a, f.ws.ID, nil, &quota) })
	f.attachFile(t, g, "12345")
	err := f.s.Tx(f.ctx, func(tx *sql.Tx) error {
		sha, _ := f.s.WriteBlob([]byte("6"))
		_, err := AddAttachment(f.ctx, tx, f.a, g, AttachmentIn{Kind: "file", Blob: sha, Size: 1, Filename: "b.txt", Mime: "text/plain"})
		return err
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over quota: %v", err)
	}
	// another workspace's use is its own: the same bytes count there too, and not here again
	var p *Project
	f.tx(t, func(tx *sql.Tx) error {
		w, err := CreateWorkspace(f.ctx, tx, f.a, "other", "Other", nil)
		if err == nil {
			p, err = CreateProject(f.ctx, tx, f.a, w.ID, "main", "Main", "OTH", nil)
		}
		return err
	})
	f.attachFile(t, f.gig(t, NewGig{ProjectID: p.ID, Title: "elsewhere"}), "12345")
	if _, _, used, _ := AttachLimits(f.ctx, f.s.R, p.WorkspaceID); used != 5 {
		t.Fatalf("other workspace uses %d, want 5", used)
	}
	usedIs(18)
}

func TestCleanFilename(t *testing.T) {
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd", `C:\Users\x\shot.png`: "shot.png", "a\x00b\u202e.txt": "ab.txt", "..": "", "/": "", "  ok.md ": "ok.md",
	} {
		if got := CleanFilename(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{10 << 20: "10 MB", 1 << 30: "1 GB", 1536: "1.5 KB", 100: "100 bytes", 1 << 40: "1 TB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}
