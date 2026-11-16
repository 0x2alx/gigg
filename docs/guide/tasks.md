# Tasks

A task belongs to one project of one workspace. Its key (for example `GIG-12`) is the project's key plus a number. When a task moves to another project it gets a new key, and the old key keeps working as an alias.

## Creating a task

Click **New task** on the board. You can also press **Ctrl+K** (⌘K on a Mac) and choose "New task".

![The new task form (Nightwalker theme)](../images/task-new-night.png)

The form is in sections:

- **From template** (when there are templates): fill the form from one, or create the task from it as is.
- **Task**: the **Title** and the **Description** (Markdown; type `@` to mention someone, and they get a notification).
- **Checklist**: one item per line.
- **Where**: **Workspace**, **Project** and **Status**. The form starts in the project in view (else the one you used last); pick any workspace and project you can add to. Moving the task to another project keeps its stage (To-Do, Doing…) in that project's statuses; moving it to another workspace clears the custom fields, the picked template and the assignees who aren't members there.
- **When**: **Start** and **Due**, dates with an optional time (a task with a start date in the future isn't picked up by agents before then), and **Repeat**, which turns the task into a recurring task (shown when you have the Schedules permission in that workspace). See [Recurring tasks](recurring.md).
- **Details**: **Priority**, **Labels** (new labels are created as you type), **Assignees** and **Colour** (the card's left edge).
- **Agent run**: model, effort, always run, allowed tools. See [Agents](agents.md).
- **Fields** (when the workspace has custom fields): their values, with a count of how many are set.

**Repeat** lays its settings out as rows, each label on the left and its controls on the right: how often (**Daily**, **Weekly**, **Monthly**), the **Schedule** to run on (or custom timing), **Every**, the days (**On**), **Time** (all day or a window **From** a time **for** a length), how many minutes early to **Create** each task, when it **Ends**, the **Options**, and the **Next** three occurrences. The **Edit schedule** dialog uses the same rows.

**Save as template** at the bottom keeps the form as a template. The expand button (two diagonal arrows, top right) grows the form to the whole page, with the task and checklist on the left and the rest on the right, the same layout as an open task's panel; your browser remembers the choice for the next time.

## The task panel

Click a card to open its panel. Its top line shows the workspace (its owner's picture and name, in its colour), the task's key, its status and its badges. The expand button (two diagonal arrows, top right) widens it, and the close button (×) or Esc closes it. Every field saves when you change it.

![A task's panel (Daywalker theme)](../images/task-panel-day.png)

The panel has the same layout as the **New task** form, each part in its own box. The task's content comes first: title and description, **Checklist**, **Subtasks** and **Comments**. Then the properties, in the form's order (**Where**, **When**, **Details**, **Agent run**), the agent session, and **Dependencies**, **Fields** and **History**. When the panel is expanded and wide enough, the content takes the left column and the rest the right.

- **Description**: click to edit. Markdown, with checkboxes you can tick in place.
- **Comments**: Markdown, with @mentions. Agents' comments are marked by kind:
  - *question*: they need an answer; the task goes to the [Inbox](inbox.md).
  - *report*: what they did.
  - *system*: the runner's own notes.
- A long description or comment shows its first part, fading out, with a **Show more** bar under it. **Show less** folds it again, and every task opens with long text folded.
- **Where**: workspace, project (move it to another project) and status. **When**: start, due, the time window and, for a recurring task's tasks, the schedule it came from. **Details**: priority, labels, assignees, colour (the card's left edge), who created it and when it last changed. **Agent run**: the agent's run settings. Fields you can't change (a viewer's) show with a dashed outline.
- **Checklist**, **Subtasks**, **Dependencies**, **Fields** and **History** are folding sections, folded when a task opens. A folded section is just its title row: click it (the arrow on the right shows which way it goes) to open it. Its badge (progress, count) shows while it's folded.
  - **Checklist**: add, tick, reorder and remove items.
  - **Subtasks**: tasks whose parent is this one. Their progress shows on the parent's card.
  - **Dependencies**: "blocks", "blocked by" and "relates to" links between tasks. A blocked task shows 🔒, and agents leave it alone until its blockers are done.
  - **Fields**: the workspace's custom fields (text, number, date, select, checkbox, link).
  - **History**: who changed what and when, newest first: edits, moves, links, checklist changes, comments and agent runs. It updates live. The admin audit log keeps the rest (which key, from where).
- **Agent session** (folding): the resume command, each run with status, duration and cost, and the transcript path. **Resume** asks the agent to continue its previous session, with your text as the next instruction. It's folded unless the agent is waiting for your answer.
- **Clone**, **Archive** / **Unarchive** and **Delete** are at the bottom of the panel.

## Time windows and missed tasks

A task can have a window: it may only be started between its start time and **Window closes**. If nobody starts it before the window closes, it's marked **missed** and its creator and assignees get a notification. Recurring tasks use windows to make sure a daily task doesn't run late.

## Archiving and deleting

- **Archive** (at the bottom of the panel, or **Archive done** on the board): the task leaves the board and lists, stays searchable, and can be unarchived.
- **Delete** moves the task to the trash. **Trash** on the board lists the deleted tasks of the projects in view, with **Restore**. A deleted task opened from a link shows **Restore** too.
- Every change is recorded with its old and new values: the API's task activity (`GET /tasks/{id}/activity`) and, for admins, **Admin → Audit**.
