# Gigs

A gig belongs to one project of one workspace. Its key (for example `GIG-12`) is the project's key plus a number. When a gig moves to another project it gets a new key, and the old key keeps working as an alias.

## Creating a gig

Click **New gig** on the board. You can also press **Ctrl+K** (⌘K on a Mac) and choose "New gig".

![The new gig form (Nightwalker theme)](../images/gig-new-night.png)

The form is an open gig's [panel](#the-gig-panel): the same editor, with the same boxes in the same order and the same rows and controls. The **New/Edit template** and **New/Edit recurring gig** dialogs are the same editor too; each shows only the boxes that apply (a template has no **When**, only a single new gig has **Dependencies**). The title and description show as the gig will read, the description folded when long (**show more** opens it), with the pencil to edit them; a new gig starts in the editor, and **Done** folds it back.

- **The gig**: **From template** (when there are templates: fill the form from one, or create the gig from it as is), the **Title** and the **Description** (Markdown; type `@` to mention someone, and they get a notification). A template also has its **Template name** here.
- **Checklist**: add items one by one, or paste several lines at once; reorder and remove them.
- **Subgigs**: gigs created together with this one. See [Subgigs and dependencies](#subgigs-and-dependencies).
- **Where**: **Workspace**, **Project** and **Status**. The form starts in the project in view (else the one you used last); pick any workspace and project you can add to. Moving the gig to another project keeps its stage (To-Do, Doing…) in that project's statuses; moving it to another workspace clears the custom fields, the dependencies, the picked template and the assignees who aren't members there.
- **When**: **Start**, **Due** and **Window closes**, dates with an optional time (a gig with a start date in the future isn't picked up by agents before then; see [time windows](#time-windows-and-missed-gigs)), and **Repeat**, which turns the gig into a recurring gig (shown when you have the Schedules permission in that workspace). See [Recurring gigs](recurring.md).
- **Details**: **Priority**, **Labels** (new labels are created as you type), **Assignees** and **Colour** (the card's left edge).
- **Agent run**: model, effort, always run, allowed tools. See [Agents](agents.md).
- **Dependencies**: gigs this one blocks, is blocked by, or relates to, linked when it's created.
- **Fields** (when the workspace has custom fields): their values, with a count of how many are set.

**Repeat** adds its settings to the **When** rows, each label on the left and its controls on the right: how often (**Daily**, **Weekly**, **Monthly**), the **Schedule** to run on (or custom timing), **Every**, the days (**On**), **Time** (all day or a window **From** a time **for** a length), how many minutes early to **Create** each gig, when it **Ends**, the **Options**, and the **Next** three occurrences. The **Edit schedule** dialog uses the same rows.

Everything you add is saved when you click **Create gig** (or **Save**): the subgigs, checklist and dependencies are made together with the gig, so a mistake in one of them creates nothing. **Save as template** at the bottom keeps the form as a template. The expand button (two diagonal arrows, top right) grows the form to the whole page, with the gig, checklist and subgigs on the left and the rest on the right, like an expanded panel; your browser remembers the choice for the next time.

## The gig panel

Click a card to open its panel. Its top line shows the workspace (its owner's picture and name, in its colour), the gig's key, its status and its badges. The expand button (two diagonal arrows, top right) widens it, and the close button (×) or Esc closes it. Every field saves when you change it.

![A gig's panel (Daywalker theme)](../images/gig-panel-day.png)

The panel has the same layout as the **New gig** form, each part in its own box, with the same controls; the panel saves each change as you make it. The gig's content comes first: title and description, **Checklist**, **Subgigs** and **Comments**. Then the properties, in the form's order (**Where**, **When**, **Details**, **Agent run**), the agent session, and **Dependencies**, **Fields** and **History**. When the panel is expanded and wide enough, the content takes the left column and the rest the right.

- **Description**: click to edit. Markdown, with checkboxes you can tick in place.
- **Comments**: Markdown, with @mentions. Agents' comments are marked by kind:
  - *question*: they need an answer; the gig goes to the [Inbox](inbox.md).
  - *report*: what they did.
  - *system*: the runner's own notes.
- A long description or comment shows its first part, fading out, with a **Show more** bar under it. **Show less** folds it again, and every gig opens with long text folded.
- **Where**: workspace, project (move it to another project) and status. **When**: start, due, the time window and, for a recurring gig's gigs, the schedule it came from. **Details**: priority, labels (type to add, × to remove), assignees (click a person to assign or unassign them), colour (the card's left edge), who created it and when it last changed. **Agent run**: the agent's run settings. Fields you can't change (a viewer's) show with a dashed outline.
- **Checklist**, **Subgigs**, **Dependencies**, **Fields** and **History** are folding sections, folded when a gig opens. A folded section is just its title row: click it (the arrow on the right shows which way it goes) to open it. Its badge (progress, count) shows while it's folded.
  - **Checklist**: add, tick, reorder and remove items.
  - **Subgigs**: gigs whose parent is this one, each with its **blocks parent** switch. Their progress shows on the parent's card. A subgig's own panel names its parent here, with its switch.
  - **Dependencies**: "blocks", "blocked by" and "relates to" links between gigs. A blocked gig shows 🔒, and agents leave it alone until its blockers are done. Subgigs that block the gig are listed under "Blocked by" too, tagged *subgig* (switch them off in **Subgigs**).
  - **Fields**: the workspace's custom fields (text, number, date, select, checkbox, link).
  - **History**: who changed what and when, newest first: edits, moves, links, checklist changes, comments and agent runs. It updates live. The admin audit log keeps the rest (which key, from where).
- **Agent session** (folding): the resume command, each run with status, duration and cost, and the transcript path. **Resume** asks the agent to continue its previous session, with your text as the next instruction. It's folded unless the agent is waiting for your answer.
- **Clone**, **Archive** / **Unarchive** and **Delete** are at the bottom of the panel. **Clone** copies the gig's content, labels, assignees, dates, custom fields and checklist (every item unticked), but not its subgigs, dependencies, comments or runs.

## Subgigs and dependencies

Both link gigs, for different reasons:

- A **subgig** is *part of* its parent: a full gig of its own (status, assignees, comments, agent runs) that belongs under one parent. The parent's card shows how many are done. A subgig added in the panel or the form is made like its parent: same project, priority, assignees, dates and agent run settings.
- A **dependency** is about *order*: "A blocks B" means B can't be started until A is done or cancelled. It links any two gigs of a workspace, whatever their parents.

By default a subgig doesn't hold anything back: agents may pick up the parent and its subgigs in any order. Tick **blocks parent** on a subgig to make its parent wait for it, like a dependency: while that subgig is open, the parent shows 🔒 and agents don't pick it up. This suits a parent that is the wrap-up of its parts ("tests pass" before "ship the release"), while a side job ("update the wiki") can stay unticked.

Neither is a queue: agents never run a chain back to back. Among the gigs that aren't held back, they take the next one by the usual order (see [Agents](agents.md#which-gig-an-agent-gets-next)). gigg refuses a link that would make a cycle, where two gigs would each wait for the other.

## Time windows and missed gigs

A gig can have a window: it may only be started between its start time and **Window closes**. If nobody starts it before the window closes, it's marked **missed** and its creator and assignees get a notification. Recurring gigs use windows to make sure a daily gig doesn't run late.

## Archiving and deleting

- **Archive** (at the bottom of the panel, or **Archive done** on the board): the gig leaves the board and lists, stays searchable, and can be unarchived. An archived gig no longer blocks anything (as a dependency or a **blocks parent** subgig).
- **Auto-archive** (in **Details**): the gig archives itself this long after it's done or cancelled (1 hour to 30 days; **never** by default). Set it on a template or a recurring gig and every gig it makes gets it. gigg checks once a minute.
- **Delete** moves the gig to the trash. **Trash** on the board lists the deleted gigs of the projects in view, with **Restore**. A deleted gig opened from a link shows **Restore** too.
- Every change is recorded with its old and new values: the API's gig activity (`GET /gigs/{id}/activity`) and, for admins, **Admin → Audit**.
