# Changelog

## Unreleased

- **Attachment viewer**: the viewer steps through a gig's attachments with ‹ › at the sides, the Left/Right arrow keys or a swipe on a phone, images and Markdown or text files alike, in the order of the Attachments section, with "3 / 12" in its head (no wrapping: the arrow at an end is hidden; the keys do nothing while typing in a field). An image clicked in a description or comment steps through the other images of that text. Images zoom with the mouse wheel or a trackpad pinch, around the pointer, and with a two-finger pinch on a phone; zoomed in, dragging pans. A double-click or double tap (or the `0` key) goes back to fit, and from fit zooms in where you clicked. The arrow keys step only at fit, and the next image opens at fit. **Fullscreen** (the button in the head, or `F`) shows the image on the whole screen; the first Esc leaves fullscreen, the second closes the viewer. One viewer serves the Attachments section and Markdown.
- **Keyboard and screen readers**: **?** shows every keyboard shortcut; **/** focuses the page's search field (else opens the command palette) and **N** starts a new gig, never while you type in a field. A **Skip to board** link is the first Tab stop on every page. Closing any dialog gives the focus back to what opened it, also when that was an item in a menu that has closed since (the menu's button gets it). Rows of colour swatches are one radio group each: one Tab stop, the arrow keys pick (the theme editor went from about 315 Tab stops to 65), and the picked colour no longer jumps to the front of its row. Esc leaves the theme editor, asking once first when there are unsaved changes. Screen readers hear what the screen shows: the permissions option that reads "Preset ✓" is "Preset, allows it", admin row buttons name their target ("Make bob admin", "Delete project OPS", "Edit field Size", "Remove bob from Ops"), a blocked card says "Blocked by 1 open gig", and the card's checklist and subgig badges read as words ("1 of 2 subgigs done").
- **Table: custom field filters and columns.** **More filters** filters by a choice, checkbox, number (=, ≥, ≤, >, < a value) or text/URL field (contains), or by **No value**; **Columns** adds field values as table columns (lines on phone cards). Both are kept in the URL and in saved views (`field.<id>`, `cols`), and the view chips name them. The filtering runs on the server, so it holds across pages: the gig lists take `field=<id>:<value>`, repeatable (a date field, or a field of another workspace, is a 422). The table's count says *N done or cancelled hidden* (it said "done hidden", and cancelled gigs were hidden too), and "1 gig".
- **Templates**: each row has **+ New gig**, which opens the New gig form filled from that template, the same as picking it under **From template**.
- **Recurring timing is checked, never changed**: Every 0, a window of 0 (or under 5 minutes, or over 31 days), a negative or too large Create early, or an impossible start time are marked on the field with what's allowed, and the form can't be saved until fixed (before, the form quietly saved other values). The API refuses `interval: 0`, `window_minutes` under 5 and a negative `lead_minutes` with a 422 naming the field (leave them out for the defaults) on recurring gigs and schedules alike.
- Recurring gig form: the window start is a 24-hour time box like the rest of the app (it takes `9:30`, `0930`, `21.30`); on phones every field is one size and the weekday chips stay on one row; **Next** says *paused* for a paused recurring gig instead of listing occurrences; and the locked **Project** says why (a recurring gig stays in the project it was made in).
- Dependencies: a cycle is reported in one sentence ("That would make a cycle: GIG-2 already depends on GIG-1."), the picker leaves out gigs that are already linked, and both the list and the picker show each gig's status by name, as everywhere else (relations carry `status_name` and `status_category`).
- Board and UI polish:
  - On a phone, a board search that leaves the selected column tab empty moves to the first tab with matches. Under a filter, tabs with matches get an accent count, and an empty tab says where the matches are ("No matches in To-Do: 1 in Needs review").
  - Archiving from the gig panel and a bulk move in the table get the same Undo toast as a card's Archive. The table's Undo puts each gig back in the status it had.
  - The phone table gets a Sort select and Select all (its header row is hidden there). In the rows layout, a lane with cards past its edge fades out and shows a "+N" button that scrolls it.
  - In the gig panel, a date picked before moving on to its time field is saved at once, so a reload keeps it. Custom fields show a new value straight away (a checkbox flips on the click), and the checkbox sits left like the other fields.
  - The profile saves every field as you change it (the Save button is gone).
  - Admin: categories read "To-Do", "Waiting" and so on, the new key dialog lists people by name, and a person without a password is marked **no password** (users gain `has_password`, sent to admins only, on humans only). `/admin/workspaces/<slug>` works with a slug as well as an id.
  - Smaller fixes:
    - the card menu's labels line up behind one icon column;
    - Templates and Import JSON have their own icons;
    - the errand Agent row shows the display name;
    - phones get a short "Add an item…" checklist placeholder;
    - the attachments usage line reads "Workspace: 109 KB of 1 GB";
    - a click anywhere on a theme card applies it;
    - the empty theme list uses body text;
    - on a phone the Repeat choice (errands, recurring gigs) is a select, and other segmented controls stay on one row instead of wrapping;
    - **Clear filters** only shows when something is filtered;
    - the template editor's help opens the templates help.

- Small wins from a QA round:
  - Cards show a paperclip with the number of attachments, next to the comment count.
  - The card's gear menu adds **Open**, **Assign to me** (adds you to the assignees) and **Copy link**; the gig panel's header has a **Copy link** button.
  - The command palette has **Recurring**, **Templates**, **Schedules**, **Errands** and **New errand**, opening what the account menu and the top bar open.
  - A subgig's card names its parent ("↳ KEY-8"), and the table's Title cell shows the blocked (🔒 n) and parent marks cards have. Gigs carry `parent_key`.
  - Template cards show their subgig count; in the template editor (and the New gig form) a subgig's title can be renamed with a click and the subgigs reordered with ↑ ↓, like checklist items.
  - Custom fields can be put in order (↑ ↓ on the workspace page). API: `POST /workspaces/{ws}/fields/reorder`.
  - A picture for your profile opens a crop dialog first: drag and zoom to pick the square to keep; it uploads that square as a 256 px PNG. The picture you choose may now be up to 20 MB.
  - **Make next now** on a recurring gig creates the next occurrence's gig at once (its window starts now), to try it without waiting; the schedule carries on after it. API: `POST /recurrences/{id}/make-now`.
- **Attachments**: a gig can carry screenshots, reports, logs and other files, links and commits, so the result of the work lives with the gig. The server decides what a file is from its bytes: images (PNG, JPEG, GIF, WebP, AVIF, BMP) open full size, with a preview made by the server (TIFF and HEIC get a preview but download), MP4 and WebM video and MP3, M4A, Ogg and WAV audio play in the panel (with range requests for seeking), Markdown reports render like comments, plain text shows as text, and everything else (HTML and SVG included) is only ever a download, served with a sandbox CSP. Limits per workspace, set by admins: 10 MB per file (50 MB at most) and 1 GB in all by default; over either is a 413 that says which. Three new per-project permissions: Download attachments (every preset), Add attachments (attach, and remove your own; consumer up) and Delete attachments (anyone's; member up). Two new key scopes: `attachments:read` (download; keys with `gigs:read` get it automatically) and `attachments:write` (keys with `gigs:work` get it). A gig in the trash hides its attachments until restored. Files sit next to user pictures in `files/`; a daily cleanup removes files nothing has used for longer than the snapshot window plus a day, so a restored snapshot always finds its files. Gigs carry `attachment_count`. API: `/gigs/{id}/attachments`, `/attachments/{id}`, `/workspaces/{ws}/attachment-limits`; MCP: `list_attachments`, `attach` (a link, a commit or a Markdown report) and `delete_attachment`, which never return contents. Migration 0021 replaces the never-used `artifacts` table. See the new [attachments guide](docs/guide/attachments.md).
- **Attachments in text**: paste or drop an image into a description or a comment and it is attached to the gig and shown in place (`![name](/api/v1/attachments/<id>/file)`, with a placeholder while it uploads); other dropped files are attached and linked. The New gig form keeps them until the gig is created, then uploads them and saves the description once. Images in Markdown fit the column and open in the same lightbox as the Attachments section; Markdown now allows attachment images and links to an attachment's file (`/api/v1/attachments/<id>/file`, optionally `?download=true`) besides http(s) and mailto. The Attachments section shows AVIF without a preview by scaling the file, plays video and audio in rows, and shows the preview of TIFF and HEIC files that download.
- **A visual editor** for descriptions and comments: text shows as it reads while you write it (bold, links, lists, task lists, tables, code, and pasted, dropped or inserted images in place), with a toolbar that shows what is on at the caret and a paperclip **Insert file** button. Markdown is still what is stored, and the **Markdown** tab edits it directly. A text the visual editor would change (a loose list, say) opens on the Markdown tab instead, so saving never rewrites what wasn't touched; the check compares what the board renders from both. @ opens the people list at the caret and inserts a plain `@username`. Links keep to what Markdown may link (http(s), mailto, an attachment's file), images load only from the server, and an image pasted into a new gig shows from the file until Create uploads it. The editor is loaded with the first gig panel, not with the board.
- **A cleaner gig panel**: the properties (Where, When, Details, Agent run) sit in one card with hairlines between the groups instead of four boxes, with smaller rows and controls. The workspace shows beside the project, Window closes appears only when set (or via **+ Window closes**), the people toggles are smaller chips (an agent is marked on its picture), the colour swatches are smaller, Agent run folds to one summary line ("opus · high effort", or "agent defaults"), and Created by and Updated are one quiet line at the bottom. Section headers, gaps and the Clone / Archive / Delete buttons are smaller too. The New gig, template and recurring gig forms share the same editor and get the same look.
- Cards: the schedule and "Done … ago" line sits under the icon row on the left, and both rows are centred on the avatars.
- **Stale gigs**: every gig carries `last_progress_at`, the time of its latest comment, report or question, checklist tick or untick, status change or finished run, whoever did it (edits to the title, description, colour, labels or followers don't count). `GET /workspaces/{ws}/stale?hours=N` (default 24) lists your gigs in Doing with no progress for that long, oldest first; MCP `stale_gigs` does the same, and `summary` ends with a line naming them. Migration 0022 adds the column, filled from the latest comment, run and done time.
- Attachments hardening: a request sent without a login or key may carry at most 1 MB (a larger one is a 401 before its body is read); an upload checks the caller's rights and the workspace's limits before any image decoding or disk write, and a failed upload removes the file it created; the daily cleanup removes unknown files under the write lock after a fresh look, so it can't race an upload of the same bytes; files are served `private, no-cache` (revalidated each time, so a revoked right takes effect at once); the MCP `delete_attachment` is offered with Delete attachments as well as Add attachments; an `Idempotency-Key` works on uploads up to the largest allowed file.
- Templates and Schedules: a template or schedule that belongs to one workspace now always shows its workspace chip, so it is plain which ones are per workspace and which are **Shared** (before, only with several workspaces).
- The account menu's **Import gigs** is now **Import JSON**: it takes every kind you may create (gigs, recurring gigs, gig templates, schedules, errands, errand templates) in one list. It still only creates.
- Board: **Clear filters** also unticks **Show archived gigs**, is active when only that box is ticked, and the More filters count includes the box.
- MCP parity for the latest release. New tools: `make_shared` (a workspace's schedule, gig template or errand template becomes shared; `dry_run` is the default and returns the plan, and confirming needs `expect_copies` from that plan, so an agent can't merge blindly; it is the `make-shared` call, so the checks are the REST ones), `import_json` (the web UI's Import JSON on the server: a list that may mix gigs, templates, schedules, errand templates, errands and recurring gigs, created in dependency order with names resolved among the rows of the list, up to 100 objects and 1 MB, a result per row made, failed or skipped, and `dry_run`; every row goes through its normal create call) and `save_errand_template`. `save_template`, `save_schedule` and `save_errand_template` take `shared: true` to make a shared one (Shared library and `library:write`, as for the API). See the [MCP reference](docs/reference/mcp.md).
- Phone and dark-mode polish: the Import JSON file picker is a button with the chosen name beside it; on phones the Schedules, Templates and Recurring actions sit in an even grid and the Where picker rows, those actions and dialog close buttons are 40 px to tap; the board's Clear filters is just its icon there (it no longer drops to its own row in the wider themes); the Import preview and Make shared lists have a visible border in dark themes; the robot badge on a card is as big as the other foot icons.
- Make shared: **Make shared…** on a workspace's schedule, gig template or errand template (Shared
  library) makes the shared item and merges the identical copies in one step. The dialog shows the plan
  first: the copies it found, the recurring gigs and errands that move, the copies it leaves alone (a
  workspace where you can't delete them) and why; **Make shared** then moves every user to the shared
  item and deletes the source and the copies, all in one transaction, with an audit row for each create,
  repoint and delete. API: `POST /schedules/{id}/make-shared`, `/templates/{id}/make-shared` and
  `/errand-templates/{id}/make-shared` (`dry_run` returns the plan). The risky settings (always-run, tool
  allowlists, run timeouts, no-sandbox) need their permission on the all-workspaces level, as for any
  shared item. Errand templates and errand schedules are shared from the errand's panel. Each recurring
  gig and errand that moves is checked in its own project, as editing it by hand would be (a copy that
  fails is skipped with a reason, a failing source user refuses the move); errand templates match only
  when who set the command and the sandbox match too; the body's `expect_copies` (sent by the dialog)
  refuses the move with 409 when the copies changed since the plan was reviewed.
- Metrics: a new page (account menu) with the numbers per day, week or month over up to 400 days, for what you can see: gigs created, done, cancelled and archived, median time from created to done, agent runs by outcome with run time, cost and tokens (also by agent, model and project), errand runs, and recurring gigs on time or missed. Pick a period, filter by workspace, project, person or humans/agents; charts are small inline SVG, no outside calls. Days are cut in your time zone. API: `GET /metrics`; MCP: `get_metrics`. Migration 0020 adds the indexes it reads. Time spent in Doing is not included. See the new [metrics guide](docs/guide/metrics.md).
- Board filters: the rows of workspace and project chips are replaced by a **Where** button in the toolbar (a searchable tree of workspaces with their projects, its face naming the selection, and a project called "Main" shown as "Workspace / Main") and one short row of shortcut chips: a chip per project not called "Main", and a removable chip for each other picked item. The URL keys are unchanged, but the picks now add up: the board shows every project of the picked workspaces plus the picked projects, so a pick can span workspaces (before, a project outside the picked workspaces was ignored).
- Import JSON and Copy as JSON: schedules, gig templates, errand templates, gigs, errands and recurring gigs (shared ones too, with the Shared library permission) copy as JSON and import from pasted JSON or a file, one object or a list (up to 100, 1 MB). The preview shows what will be made, where, and skips rows that don't resolve (a wrong kind, a missing field, an unknown username, a template or schedule name that matches nothing); Create then makes each object through the normal create call, so validation and permissions are the forms' own, and shows the result per row. One list can hold schedules, templates and the recurring gigs that name them: rows are created in dependency order (schedules, templates, then what uses them, then gigs), and a name from the same list resolves to the row just made. Ids, workspaces and `created_*` in the JSON are ignored. A refused request now names the field (`window_start: ...`) instead of just "validation failed". See the new [import guide](docs/guide/import-export.md).
- Board cards: an amber bolt pill marks an Always run gig (also in the gig panel), and the robot badge on a card now shows for any gig with an agent run, its tooltip giving the last run's outcome, the worker and when.
- Shared library permission: the shared gig templates, schedules and errand templates no longer need
  an instance admin. The new `shared_library` permission (Planning) makes, changes and deletes them; it
  counts only on an all-workspaces rule (a workspace or project rule can't hold it), needs view on that
  level, in every workspace and in every project, and is in no preset. The run settings inside a shared item
  (always-run, tool allowlists, run timeouts, no-sandbox) need their own permission on the
  all-workspaces level too. Keys need the new `library:write` scope next to the scope of the kind
  (`gigs:write` for gig templates and schedules, `errands:write` for errand templates) and must not be
  limited to some places; existing unlimited admin keys with `gigs:write` or `errands:write` get
  `library:write` in migration 0019, which changes nothing they could do before. A project hidden from
  the holder takes the permission away like a hidden workspace; retiming a shared schedule that errands
  use needs Errands on the all-workspaces level and `errands:write`.
  `/auth/me` lists `all_workspaces` (the permissions held on that level); the permissions editor shows
  Shared library only on **All workspaces**, and the Shared toggles, edit buttons and **Shared
  templates** follow the permission instead of the admin flag.
- Followers: anyone who can see a gig can follow it (**Follow** at the top of the gig panel, or named in
  its **Followers** in the gig form, the API and MCP), and gets its bells as its assignees do. A gig's
  bells go to its assignees and followers, no longer to its creator as such: you follow the gigs you
  create unless you turn off **Follow the gigs I create** (Profile → Bell). Templates carry followers,
  so gigs made from one, recurring ones included, start with them. API: `followers` on gigs,
  templates and recurring gigs, `GET/PUT/DELETE /gigs/{id}/follow`; MCP: `follow_gig`. Existing gigs
  start with no followers.
- Bell settings (Profile → Bell): pick which events ring you: needs a person (questions, and the new
  **failed** kind: a run that ended in an error or timed out), finished (reports; off by default),
  assigned, mentioned, missed window. A timed-out run used to ring nobody.
- Audit log: every object records the same thing. An update records each field that changed with its old
  and new value (schedules, templates, recurring gigs, errands and errand templates recorded only a few
  fields before; workspaces, projects and statuses only the new values), a create every initial value, a
  delete the last values. One store helper diffs the object as read before and after the write, so new
  fields are audited without more code. Errand commands are recorded old and new, with secrets masked.
  **Admin → Audit** shows a change as `field: old → new`. In workspace activity, the change feed and live
  events, errand entries carry their content only for readers with `errands:read`.
- Cards: a gig a recurring gig made stands out: a glowing purple **Recurring** badge on its top
  line, and a purple-tinted card and border. The hue is the same in every theme, the strength follows
  the theme, and the badge text reads at 4.5:1 in every built-in theme. The small ↻ pill in the card's
  foot is gone (the gig panel keeps it).
- Cards: the gig's key sits in the card's top right corner and always stays visible. The card's
  menu is a **gear** at the start of the foot, always shown (no longer only on hover), level with the
  foot's row; a done card's archive button follows it.
- Filters: one **More filters** button on the board and in the table holds the pickers that used to
  fill the toolbar (assignee, label, priority, recurring, and archived on the board; status, label,
  assignee, priority, recurring and include done in the table), with a count of those that are on.
- Filters: **Always run** on the board and in the table: every gig, **Hide always run** or **Only
  always run**. Kept like the recurring filter (address, remembered board filter, saved views); the
  gig list API and MCP `list_gigs` take `always_run=hide|only`.
- Filters: **Recurring** on the board and in the table: every gig, **Hide recurring** (the gigs a
  recurring gig made) or **Only recurring**. It is kept in the page address, in the board's
  remembered filter and in saved views; the gig list API and the MCP `list_gigs` tool take
  `recurring=hide|only`.
- Gig panel: the **Checklist** section opens on its own when the gig has items, and stays folded
  when it has none; switching to another gig starts from that gig's checklist. The new-gig form and
  the template editor do the same, and picking a template with a checklist opens it.
- Cards: when a person handed a gig to an agent, hovering the person's picture, the **›** or the
  agent's picture reads it as one sentence ("Dana Rivera handed this gig to Scout").
- Docs: the README opens with a transparent logo (light and dark variants) and tells gigs
  (prompt-based, for agents to think through) from errands (commands run on a schedule); the themes
  guide covers **Make with AI**.
- Brand: the wordmark and the link preview's tagline are set in Anta (SIL OFL), bolder, with the
  wordmark centred on the ship; the banner, the link preview and the README header follow.
- PostgreSQL: someone who views a workspace while project rules hide all of its projects got a 500
  from the activity list, the change feed, the event stream and the MCP `changes` tool (an empty
  `IN ()`, which PostgreSQL refuses); they now see an empty list. Every dynamic `IN` list goes
  through one helper, `dev/check.sh` refuses hand-built ones, and the new `dev/check-pg.sh` runs the
  store and api tests against a throwaway PostgreSQL.
- Errands every N minutes or hours: on the days clocks change, the daily window now ends at its local
  clock time, not after a fixed number of minutes. A whole-day hourly errand lost its 23:00 run on
  the day clocks go back; a window running past the change ended an hour off.
- A gig moved to another project now also logs `gig.moved_out` in the project it left, so someone
  who sees only that project gets the change on the feed and the live board drops the card (before,
  it stayed until a reload). The entry names only the old key.
- Attachment quota: a workspace's use is now what its attachments take on disk. Image previews
  count, and a file attached several times in a workspace (stored once) counts once; the upload
  check counts the same way, preview included.

## 0.1.0 (2026-11-19)

The first public release.

- **A board** for gigs across workspaces and projects: columns or rows, filters kept in the URL,
  manual order and per-column sorting, archive and trash, live updates, a command palette (Ctrl+K),
  tables with bulk actions and saved views. It works on a phone too.
- **Gigs**: Markdown with @mentions, checklists, subgigs, dependencies, custom fields, labels,
  colours, start and due dates, time windows, assignees.
- **Recurring gigs** built from reusable templates and schedules, per workspace or shared.
- **Agents** as ordinary users. They get runs with status, cost and reports, resumable sessions,
  questions in an inbox, and per-gig model, effort, *always run* and tool allowlists. Each
  workspace has daily caps and a pause switch, and the dashboard shows their runs.
- **Errands**: shell commands an agent runs on a schedule in its sandbox, with a run history and
  each run's output. The executor that runs them is outside gigg: the server never runs commands.
- **The same features three ways:** the web UI, a REST API with an OpenAPI document, and MCP.
- **Access control**: workspaces, preset roles, per-permission overrides for users and groups (per
  workspace, per project or everywhere), and API keys limited by workspace, project, scope,
  listener, network and expiry.
- **One binary** with SQLite: HTTPS, an optional second listener for agents, nightly backups and
  snapshots before migrations.
- **Themes** (built-in and your own), built-in help and an audit log.
