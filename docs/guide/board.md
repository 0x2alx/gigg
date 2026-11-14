# The board

The board shows the tasks of every workspace and project you can see, in one place. Open it from **Board** in the sidebar.

![The board across several workspaces (Nightwalker theme)](../images/board-all-night.png)

## Columns

- **Several projects in view:** one column per stage: Backlog, To-Do, Doing, Waiting and Done. A column takes a project's own name for a stage when every project in view uses the same name; otherwise it shows the stage's general name, and each card shows its project's status name (for example "Needs review") when it differs. Cards are ordered by priority, then by the earliest due date.
- **One project in view:** that project's own statuses as columns, in the order you set. Drag cards up and down to reorder them, or use the card's **⋯** menu (To top, Up, Down, To bottom).

Cancelled tasks don't appear on the board. Use the table view to see them.

Click the **‹** in a column header to collapse the column to a narrow strip (the board remembers this per browser). Click the strip to open it again. On a phone, the columns are tabs above the cards, and the filters fold away under **Filters**.

![The board on a phone (Daywalker theme)](../images/board-mobile-day.png)

## Cards

Each card shows:

| Mark | Meaning |
|---|---|
| Coloured name (first badge) | The task's workspace, on the workspace's colour |
| `KEY-12` | The task's key: the project's key and the task's number |
| ▾ ▸ ▴ ⏶ | Priority: Low, Medium, High, Urgent |
| 🔒 n | Blocked by n open tasks; agents won't pick it up until they're done |
| ☑ 2/5 | Checklist progress |
| ⑂ 1/3 | Subtasks done |
| ⟳ | Made by a recurring task |
| window / missed | The time window it may be started in; missed if it closed untouched |
| ⏱ date | Due date (red when overdue) |
| 💬 n | Comments |
| agent | An agent has worked on it (it has an agent session) |
| $0.42 | What the agent's runs cost so far |
| resume queued | Someone asked the agent to continue its session |

The avatars at the bottom show who created the task → who it's assigned to. Hover over (or long-press) any badge to see its tooltip.

## Moving tasks

- Drag a card to another column. With several projects in view, the task goes to its project's first status of that stage (for example, the first "Done" status).
- Or use the card's **⋯** menu: **Move to** lists the task's project statuses, and **Move to project** lists the other projects of its workspace.
- Click a card to open the task panel (see [Tasks](tasks.md)).

## Filters

![Filters: one workspace, a label and a priority (Daywalker theme)](../images/board-filters-day.png)

The row above the columns narrows the board:

- **Workspaces** and **Projects**: click a chip to show only that one; click more chips to add them. **All workspaces** / **All projects** clears the selection. The workspace chips show the workspace's owner picture and colour.
- **Assignee**: anyone, assigned to you, unassigned, or a person.
- **Label** and **Priority** (a minimum: "High or higher").
- **Search**: text in the key, title or description.
- **Clear filters** resets everything except the archived switch.

The filters live in the page address (for example `/board?ws=acme&pr=3`), so you can bookmark or share a filtered board. The board also remembers the last filter you used.

## The buttons at the top

- **Table**: the same tasks as a sortable table (per workspace), with bulk actions. See [Views](views.md).
- **Recurring**, **Schedules**, **Templates**: see [Recurring tasks](recurring.md).
- **Archive done**: archives the tasks in Done (all of them, or those done for 7 or 30+ days). Archived tasks leave the board and lists but stay searchable, and you can unarchive them from the task.
- **Show archived**: shows archived tasks, greyed out, in their columns.
- **Trash**: the deleted tasks of the projects in view, most recently changed first, each with **Restore**, which puts it back in its column. Restoring needs the *Delete tasks* permission.

![The trash (Daywalker theme)](../images/trash-day.png)
- **New task**: with one project in view it opens the form right away; otherwise it first asks which project.

Buttons that act on one project (Recurring, Templates, Archive done, New task) ask which project when several are in view. Schedules asks which workspace.
