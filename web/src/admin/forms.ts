// Pure helpers for the admin forms: slugs and usernames suggested from a display name.

// slugify: "Café Ops & Infra" -> "cafe-ops-infra". Lowercase a-z, 0-9 and single dashes, starting
// with a letter, at most 32 characters, no trailing dash (the server's slug rule).
export function slugify(s: string, max = 32): string {
  return s
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^[^a-z]+/, "")
    .slice(0, max)
    .replace(/-+$/, "");
}

// usernameFrom: a username suggestion from a display name ("Ada Lovelace" -> "ada-lovelace").
export function usernameFrom(name: string): string {
  return slugify(name);
}

// followName: the value a name-derived field should take after the name changes. It keeps following
// the name until the user edits it by hand ("touched"), and starts following again once they clear it.
export function followName(name: string, current: string, touched: boolean, derive: (n: string) => string = slugify): string {
  return touched && current !== "" ? current : derive(name);
}

export const PICTURE_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif"];
export const PICTURE_MAX = 2 * 1024 * 1024;

// pictureProblem: why a picture can't be uploaded (null when it can). Mirrors the server's rules.
export function pictureProblem(f: { size: number; type: string }): string | null {
  if (!PICTURE_TYPES.includes(f.type)) return "Use a PNG, JPEG, WebP or GIF image.";
  if (f.size > PICTURE_MAX) return "Pictures are up to 2 MB.";
  if (f.size === 0) return "That file is empty.";
  return null;
}
