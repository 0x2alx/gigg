import { describe, expect, it } from "vitest";
import type { Attachment } from "./api";
import { attachmentName, canRemove, fileURL, fmtSize, groupAttachments, refKind, shortCommit, shownAs, thumbURL, tileSrc } from "./attachments";

const att = (p: Partial<Attachment>): Attachment => ({
  id: 1, gig_id: 1, kind: "file", label: "", value: "", filename: "a.bin", mime: "application/octet-stream", size: 10, width: null, height: null,
  inline: false, has_thumb: false, created_by: 2, created_by_username: "scout", run_id: null, created_at: 0, ...p,
});

describe("attachments", () => {
  it("shows each kind its own way", () => {
    expect(shownAs(att({ mime: "image/png", inline: true, has_thumb: true }))).toBe("image");
    expect(shownAs(att({ mime: "image/png", inline: true }))).toBe("image"); // no thumb: still opens the image
    expect(shownAs(att({ mime: "text/markdown", inline: true }))).toBe("markdown");
    expect(shownAs(att({ mime: "text/plain", inline: true }))).toBe("text");
    expect(shownAs(att({ mime: "image/avif", inline: true }))).toBe("image"); // no preview: the tile shows the file
    expect(shownAs(att({ mime: "image/bmp", inline: true }))).toBe("image");
    expect(shownAs(att({ mime: "video/mp4", inline: true }))).toBe("video");
    expect(shownAs(att({ mime: "video/webm", inline: true }))).toBe("video");
    expect(shownAs(att({ mime: "audio/mpeg", inline: true }))).toBe("audio");
    expect(shownAs(att({ mime: "audio/ogg", inline: true }))).toBe("audio");
    expect(shownAs(att({ mime: "image/tiff", inline: false, has_thumb: true }))).toBe("file"); // a download, with its preview
    expect(shownAs(att({ mime: "image/heic", inline: false, has_thumb: true }))).toBe("file");
    expect(shownAs(att({ mime: "video/quicktime", inline: false }))).toBe("file");
    expect(shownAs(att({ mime: "image/svg+xml", inline: false }))).toBe("file"); // not served in the page
    expect(shownAs(att({ mime: "application/pdf" }))).toBe("file");
    expect(shownAs(att({ mime: null }))).toBe("file");
    expect(shownAs(att({ kind: "link", value: "https://example.com", filename: null, mime: null }))).toBe("link");
    expect(shownAs(att({ kind: "commit", value: "abcdef1", filename: null, mime: null }))).toBe("commit");
  });

  it("groups them in the server's order", () => {
    const items = [
      att({ id: 1, kind: "link", value: "https://a" }),
      att({ id: 2, mime: "image/png", inline: true }),
      att({ id: 3, mime: "text/markdown", inline: true }),
      att({ id: 4 }),
      att({ id: 5, mime: "image/jpeg", inline: true }),
      att({ id: 6, kind: "commit", value: "abcdef1" }),
      att({ id: 7, mime: "video/mp4", inline: true }),
      att({ id: 8, mime: "audio/wav", inline: true }),
      att({ id: 9, mime: "image/tiff", has_thumb: true }),
    ];
    const g = groupAttachments(items);
    expect(g.images.map((a) => a.id)).toEqual([2, 5]);
    expect(g.media.map((a) => a.id)).toEqual([7, 8]);
    expect(g.docs.map((a) => a.id)).toEqual([3, 4, 9]);
    expect(g.refs.map((a) => a.id)).toEqual([1, 6]);
  });

  it("says sizes in words", () => {
    expect(fmtSize(0)).toBe("0 B");
    expect(fmtSize(512)).toBe("512 B");
    expect(fmtSize(1024)).toBe("1 KB");
    expect(fmtSize(4300)).toBe("4.2 KB");
    expect(fmtSize(12 * 1024 * 1024)).toBe("12 MB");
    expect(fmtSize(1.5 * 1024 * 1024)).toBe("1.5 MB");
    expect(fmtSize(1 << 30)).toBe("1 GB");
    expect(fmtSize(1024 * 1024 - 1)).toBe("1 MB");
    expect(fmtSize(-1)).toBe("");
  });

  it("tells a commit id from a link", () => {
    expect(refKind("9e29e9c")).toBe("commit");
    expect(refKind(" 9E29E9C0FFEE ")).toBe("commit");
    expect(refKind("abc12")).toBe("link"); // too short for a commit
    expect(refKind("https://example.com/pr/7")).toBe("link");
    expect(shortCommit("9e29e9c0ffee1234")).toBe("9e29e9c");
  });

  it("names them, label first", () => {
    expect(attachmentName(att({ filename: "report.md" }))).toBe("report.md");
    expect(attachmentName(att({ filename: "report.md", label: "Weekly report" }))).toBe("Weekly report");
    expect(attachmentName(att({ kind: "commit", value: "9e29e9c0ffee", filename: null }))).toBe("9e29e9c");
    expect(attachmentName(att({ kind: "link", value: "https://example.com", filename: null }))).toBe("https://example.com");
  });

  it("lets you remove your own with Work on gigs, anyone's with Edit gigs", () => {
    const a = att({ created_by: 2 });
    expect(canRemove(a, 2, true, false)).toBe(true);
    expect(canRemove(a, 3, true, false)).toBe(false);
    expect(canRemove(a, 3, false, true)).toBe(true);
    expect(canRemove(a, 2, false, false)).toBe(false);
  });

  it("builds the file URLs", () => {
    expect(fileURL(7)).toBe("/api/v1/attachments/7/file");
    expect(fileURL(7, true)).toBe("/api/v1/attachments/7/file?download=true");
    expect(thumbURL(7)).toBe("/api/v1/attachments/7/thumb");
    expect(tileSrc({ id: 7, has_thumb: true })).toBe("/api/v1/attachments/7/thumb");
    expect(tileSrc({ id: 7, has_thumb: false })).toBe("/api/v1/attachments/7/file"); // AVIF: the file, scaled

  });
});
