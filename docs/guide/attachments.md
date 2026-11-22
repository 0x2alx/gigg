# Attachments

A gig can carry what was made for it: screenshots, reports, logs and other files, links and commits. They sit in the gig's panel under **Attachments**, so the result of the work lives with the gig instead of in a folder somewhere.

## What you can attach

- **Images** (PNG, JPEG, GIF, WebP, AVIF, BMP) show as a grid of previews. Click one to see it full size. TIFF and HEIC photos get a preview too, but browsers can't show them full size, so they download.
- **Video** (MP4, WebM) and **audio** (MP3, M4A, Ogg, WAV) play in the panel.
- **Markdown reports** (`.md`) open in a viewer, rendered like comments. **Plain text** (logs, notes) opens as text.
- **Other files** (PDF, archives, QuickTime video, HTML, SVG, anything else) are listed with their size and download when clicked. gigg never shows them in the page.
- **Links** (an `http` or `https` address) open in a new tab.
- **Commits** (a commit id, 7 to 64 hex digits) show their short id.

Each can have a short label. The server decides what a file is from its contents, not from its name: a file called `shot.png` that holds a web page is treated as a web page, which means download only.

## Adding and removing

- In the panel, **Attach** picks one or more files, or drag files onto the panel. **Add link** takes a link or a commit id.
- Agents attach through the API or MCP (see below). A runner can upload what the agent leaves in its run's outbox folder when the run ends; those attachments name the run they came from.
- Each action has its own permission, per project, so you can give exactly what someone needs:
  - **Download attachments**: open and download files, and see images in descriptions and comments. Without it the attachments still show, by name. Every preset has it.
  - **Add attachments**: attach files, links and commits, and remove your own. From the consumer preset up.
  - **Delete attachments**: remove anyone's. From the member preset up.
- Everyone who can see the gig sees its attachments.

## In descriptions and comments

- Paste a screenshot or drop an image into the description editor or the comment box: it is attached to the gig and shown in the text, as `![shot.png](/api/v1/attachments/12/file)`. While it uploads the text holds a placeholder (`![Uploading shot.png…]()`); if the upload fails the placeholder goes and the reason shows under the editor. Other files dropped there are attached and linked: `[report.pdf](/api/v1/attachments/13/file)`.
- In the **New gig** form the gig doesn't exist yet, so pasted and dropped files wait: they go up right after **Create gig**, and the description gets their addresses.
- Images in text fit the column; click one to see it full size. Links to an attachment's file (`/api/v1/attachments/<id>/file`, optionally with `?download=true`) are the only links to this server that Markdown keeps, and images may come only from this server.
- Agents can show or link an attachment in a comment or description the same way: `![](/api/v1/attachments/<id>/file)` for an image, `[label](/api/v1/attachments/<id>/file)` for any file. The id comes from `attach` or `list_attachments`.

## Limits

Each workspace has two limits, set by an admin in **Admin → Workspaces** (see [Administration](admin.md#workspaces)):

- the largest file: 10 MB by default, 50 MB at most;
- the total of all its attachments: 1 GB by default.

An upload over either is refused with a message saying which limit it hit. The same file attached twice is stored once on disk, but counts toward the total each time.

## Archive, trash and removal

- An archived gig keeps its attachments.
- A gig in the trash hides them; restoring the gig brings them back.
- A removed attachment is gone from the gig at once. Its file stays on disk until nothing has used it for longer than the nightly snapshot window, so restoring a snapshot never points at a missing file. Then the daily cleanup removes it. A deleted project or workspace takes its attachments with it in the same way.

## Backups and export

Files are stored under the data folder in `files/`, next to user pictures; back up that folder with the database snapshots (see [Configuration](../reference/configuration.md#backups)). **Copy as JSON** and **Import JSON** don't include attachments.

## For agents and programs

- REST: `GET /gigs/{id}/attachments`, `POST /gigs/{id}/attachments?filename=…` with the file as the request body, `POST /gigs/{id}/attachments/link` for links and commits, `GET /attachments/{id}/file`, `DELETE /attachments/{id}`. Keys need the `attachments:read` scope to download a file and `attachments:write` to add or remove. See the [REST API](../reference/api.md#attachments).
- MCP: `list_attachments`, `attach` (a link, a commit, or a short Markdown report) and `delete_attachment`. See [MCP](../reference/mcp.md#attachments).
- The MCP tools give an agent attachments by name, kind and size, never a file's contents, so a file can't slip instructions into an agent's context unasked or fill it up. An agent that needs a file downloads it with its key (`attachments:read` and Download attachments) and opens or searches it on disk, on demand.
