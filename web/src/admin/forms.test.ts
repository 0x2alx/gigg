import { describe, expect, it } from "vitest";
import { followName, pictureProblem, slugify, usernameFrom } from "./forms";

describe("slugify", () => {
  it("lowercases and dashes", () => {
    expect(slugify("Ops & Infra")).toBe("ops-infra");
    expect(slugify("  Home  lab  ")).toBe("home-lab");
  });
  it("drops accents instead of letters", () => {
    expect(slugify("Café Crème")).toBe("cafe-creme");
  });
  it("starts with a letter", () => {
    expect(slugify("42 Robots")).toBe("robots");
    expect(slugify("---x")).toBe("x");
  });
  it("caps the length without a trailing dash", () => {
    const s = slugify("a".repeat(31) + " b");
    expect(s.length).toBeLessThanOrEqual(32);
    expect(s.endsWith("-")).toBe(false);
    expect(slugify("x".repeat(40)).length).toBe(32);
  });
  it("is empty for nothing usable", () => {
    expect(slugify("123 !!")).toBe("");
    expect(slugify("")).toBe("");
  });
  it("matches the server pattern when long enough", () => {
    for (const n of ["My Workspace", "Ünïcödé Team 2", "R2-D2 fan club"]) expect(slugify(n)).toMatch(/^[a-z][a-z0-9-]{1,31}$/);
  });
});

describe("usernameFrom", () => {
  it("suggests a username from a display name", () => {
    expect(usernameFrom("Ada Lovelace")).toBe("ada-lovelace");
    expect(usernameFrom("Claude (review bot)")).toBe("claude-review-bot");
  });
});

describe("followName", () => {
  it("follows the name until edited by hand", () => {
    expect(followName("New Name", "old", false)).toBe("new-name");
    expect(followName("New Name", "custom", true)).toBe("custom");
  });
  it("follows again once the field is cleared", () => {
    expect(followName("New Name", "", true)).toBe("new-name");
  });
});

describe("pictureProblem", () => {
  it("accepts the four image types up to 2 MB", () => {
    for (const type of ["image/png", "image/jpeg", "image/webp", "image/gif"]) expect(pictureProblem({ type, size: 1000 })).toBeNull();
    expect(pictureProblem({ type: "image/png", size: 2 * 1024 * 1024 })).toBeNull();
  });
  it("refuses other types, big and empty files", () => {
    expect(pictureProblem({ type: "image/svg+xml", size: 100 })).toMatch(/PNG/);
    expect(pictureProblem({ type: "image/png", size: 2 * 1024 * 1024 + 1 })).toMatch(/2 MB/);
    expect(pictureProblem({ type: "image/png", size: 0 })).toMatch(/empty/);
  });
});
