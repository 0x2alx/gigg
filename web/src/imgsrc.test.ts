import { describe, expect, it } from "vitest";
import { imageSrcOK } from "./imgsrc";

describe("imageSrcOK", () => {
  it("takes avatars and static image files", () => {
    for (const s of ["/api/v1/users/3/avatar", "/api/v1/users/3/avatar?v=17", "/og-image.png", "/icons/a-b_c.svg", "/x.JPG"]) expect(imageSrcOK(s), s).toBe(true);
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
    ])
      expect(imageSrcOK(s), s).toBe(false);
  });
});
