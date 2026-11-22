import { describe, expect, it } from "vitest";
import { attachmentHrefOK, attachmentOf, imageSrcOK } from "./imgsrc";

describe("imageSrcOK", () => {
  it("takes avatars and static image files", () => {
    for (const s of ["/api/v1/users/3/avatar", "/api/v1/users/3/avatar?v=17", "/og-image.png", "/icons/a-b_c.svg", "/x.JPG", "/api/v1/attachments/12/file", "/api/v1/attachments/12/thumb"]) expect(imageSrcOK(s), s).toBe(true);
  });
  it("refuses other API calls, streams, other sites and odd paths", () => {
    for (const s of [
      "/api/v1/workspaces/home/events",
      "/api/v1/gigs/1",
      "/api/v1/users/3/avatar/../../x.png",
      "//evil.example/x.png",
      "https://evil.example/x.png",
      "/../x.png",
      "/x.png?y=1",
      "data:image/png;base64,AAAA",
      "/x.html",
      "/./api/v1/x.png",
      "/api/v1/attachments/abc/file",
      "/api/v1/attachments/12/file?download=true",
      "/api/v1/attachments/12/text",
      "/api/v1/attachments/12/../../gigs/1",
      "/api/v1/attachments/12/thumb/x.png",
      "/api/v1/attachments/-1/file",
    ])
      expect(imageSrcOK(s), s).toBe(false);
  });
});

describe("attachmentHrefOK", () => {
  it("takes an attachment's file, as is or as a download", () => {
    for (const s of ["/api/v1/attachments/3/file", "/api/v1/attachments/3/file?download=true"]) expect(attachmentHrefOK(s), s).toBe(true);
  });
  it("refuses any other same-origin path", () => {
    for (const s of ["/api/v1/attachments/3/thumb", "/api/v1/attachments/x/file", "/api/v1/attachments/3/file?download=1", "/api/v1/gigs/3", "//evil.example/api/v1/attachments/3/file", "/api/v1/attachments/3/file/../../../x", "javascript:alert(1)"])
      expect(attachmentHrefOK(s), s).toBe(false);
  });
  it("reads the attachment id", () => {
    expect(attachmentOf("/api/v1/attachments/42/thumb")).toBe(42);
    expect(attachmentOf("/api/v1/attachments/42/file?download=true")).toBe(42);
    expect(attachmentOf("/og-image.png")).toBe(null);
  });
});
