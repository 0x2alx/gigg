# The board

The board shows the tasks of every workspace and project you can see, in one place. Open it from **Board**, the first and largest entry in the sidebar.

The sidebar is a narrow strip of icons in two panels: at the top **Board**, **Dashboard** and **My tasks** (with your saved views); at the bottom **Admin**, **Themes**, your profile and **Log out**. Point at a panel (or tab into it) and it opens over the page with its labels, without moving the page; it folds back a moment after the pointer leaves. Each panel opens on its own. The page you are on is filled with the theme's accent colour.

**Inbox** (an envelope) and **Notifications** (a bell) sit in the top-right corner of every page, each with its count, beside the live-connection dot. See [Inbox and notifications](inbox.md). On a phone the sidebar is a menu behind the button at the top left.

![The board across several workspaces (Nightwalker theme)](../images/board-all-night.png)

## Columns

- **Several projects in view:** one column per stage: Backlog, To-Do, Doing, Waiting and Done. A column takes a project's own name for a stage when every project in view uses the same name; otherwise it shows the stage's general name, and each card shows its project's status name (for example "Needs review") when it differs. Cards are ordered by priority, then by the earliest due date.
- **One project in view:** that project's own statuses as columns, in the order you set. Drag cards up and down to reorder them, or use the card's **⋯** menu (To top, Up, Down, To bottom).

Cancelled tasks don't appear on the board. Use the table view to see them.

Each column header shows the column's name on a label filled with its colour, with a small mark for the stage (a dashed ring for Backlog, an empty ring for To-Do, half full for Doing, a pause bar for Waiting, a check for Done), then its number of tasks. Hovering over the header (or tabbing to it) shows two buttons on the right:

- **+**: a new task in this column. The form starts in this column's status; if you move the task to another project in the form, it starts in that project's first status of the same stage.
- **Collapse** (two arrows pointing in): the column leaves the board and becomes a small button above it, with its name and count. Click the button to bring the column back. The board remembers collapsed columns per browser. You can drop a card on a collapsed column's button too.

Each column also ends with **+ New task**, except Done: when it holds tasks it ends with **Archive done** instead, which opens the archive dialog for done tasks only (see **Archive done** below). Cancelled tasks never show on the board; **⋯ → Archive done** archives them too. On a phone, the columns are tabs above the cards.

### Agent activity

When agents are at work on the tasks in view, a line under the filters sums it up: how many tasks are **running**, how many **need an answer** (the agent asked a question or left a report and waits; the link opens the [Inbox](inbox.md)), how many **failed**, and how many have a resume queued.

![The board on a phone (Daywalker theme)](../images/board-mobile-day.png)

## Cards

A card is quiet on purpose: the theme's border colour and width, the title first, a few facts under it. When someone else or an agent moves a card, it slides to its new place. Hover over (or long-press) any mark to see its tooltip.

- **Top row:** the priority as an icon (three bars for Low, Medium and High, a square with "!" for Urgent; High and Urgent are coloured), the agent state if there is one (see below), and, when several workspaces are in view, the task's workspace (a dot in its colour and its name). When the column is a stage and the task's own status is named differently (for example "Needs review"), that status follows in grey. The task's key (`KEY-12`: the project's key and the task's number) sits at the right, small; the card's **⋯** takes its place when you hover.
- **Title:** up to two lines.
- **Due date and labels:** the due date as a small outlined chip with a calendar (red when overdue), then up to two labels as tinted chips; "+3" stands for the rest (hover to list them).
- **Agent block** (only when there is something to show, see below).
- **Bottom row:** under a thin line, facts as small grey icons and numbers, then the people.
- **The task's colour:** a coloured stripe on the card's left edge appears only when the task has a colour. A card that is blocked or missed has a red border, and an archived card a dashed one.
- **⋯** (top right, shown when you hover the card or tab to it, and always on a touch screen): move the card (see below).

| Mark | Meaning |
|---|---|
| padlock n (red) | Blocked by n open tasks; agents won't pick it up until they're done |
| checklist 2/5 | Checklist progress |
| branches 1/3 | Subtasks done |
| circling arrows | Made by a recurring task |
| clock window / missed (red) | The time window it may be started in; missed if it closed untouched |
| speech bubble n | Comments |
| robot | An agent has worked on it (it has an agent session) |
| $0.42 | What the agent's runs cost so far |
| a tinted chip | A custom field value (select or checkbox) |

### Agent state

A card shows what its agent is doing, as a small badge in the top row and, for some states, a block under the title:

| Badge | Block | Meaning |
|---|---|---|
| **Running** · 4m 12s (green, pulsing dot) | the agent, its model, a progress bar and its latest progress note | An agent is working on it now; the time counts up |
| **Needs answer** (question mark) | the agent's question ("asks") or report ("reports"), up to three lines | The task is in Waiting and has a question or report that nobody else has answered yet: open it and answer, then **Resume**. Your answer clears the badge; the agent's own follow-ups don't |
| **Failed**, **Timed out** or **Interrupted** (red, warning sign) | a red block at the foot of the card, with the reason and **Retry** | The agent's last run didn't finish. **Retry** sends the task back to To-Do: the runner resumes the agent's session on its next pass (or starts afresh when there is none). Archived tasks can't be retried |
| **Resume queued** (circular arrows) | none | Someone asked the agent to continue its session |

The progress bar and note appear when the agent reports progress (the `report_progress` MCP tool or the runner's progress call) and update live. Archived cards show no agent state. Collapsing a column moves the keyboard focus to its button above the board, and expanding it moves the focus back to the column's name.

The avatars at the bottom right are the people assigned to the task, overlapping. When there is no assignee, the creator is shown; when a person handed the task to an agent, the creator appears as a smaller picture before the agent. Hover an avatar to see who created the task and who it is assigned to. Agents have square-ish avatars.

## Moving tasks

- Drag a card to another column. With several projects in view, the task goes to its project's first status of that stage (for example, the first "Done" status).
- Or use the card's **⋯** menu: **Move to** lists the task's project statuses, and **Move to project** lists the other projects of its workspace.
- Click a card to open the task panel (see [Tasks](tasks.md)).

## Filters

![Filters: one workspace, a label and a priority (Daywalker theme)](../images/board-filters-day.png)

The toolbar above the columns narrows the board, in one row:

- **Search**: text in the key, title or description.
- **Assignee**: anyone, assigned to you, unassigned, or a person.
- **Label** and **Priority** (a minimum: "High or higher"). A filter that is on is tinted.
- **Archived**: a toggle that shows archived tasks, greyed out, in their columns.
- **Clear filters**: appears only while a filter is on, and resets everything except the archived toggle.

With the filters open, small chips under the toolbar scope the board: **Workspaces** and **Projects**. Click a chip to show only that one; click more chips to add them. **All workspaces** / **All projects** clears the selection. The workspace chips show the workspace's owner picture and colour.

**Filters** at the start of the toolbar folds the filters away: what stays is one line with the filters that are on, each as a chip with **×** to remove it (and **Clear all** when there are several), or just the **Filters** button with nothing set. While folded, a number on the button counts the filters that are on. They start folded; the board remembers when you open them (on a phone they start folded each time).

The filters live in the page address (for example `/board?ws=acme&pr=3`), so you can bookmark or share a filtered board. The board also remembers the last filter you used, including one you arrived with from a link or a dashboard card.

## The buttons at the top

Next to the title (and its **?** help button) the header has three things:

- **Board | Table**: a switch between this board and the same tasks as a sortable table, with bulk actions: the project in view, the workspace in view, or every workspace. See [Views](views.md).
- **+ New task**: opens the form right away, in the project in view (with several in view, the one you last added a task to). The form's **Where** section moves it to any project you can add to.
- **⋯** (More actions): the less frequent actions.
  - **Recurring**, **Schedules**, **Templates**: see [Recurring tasks](recurring.md).
  - **Archive done**: archives the tasks in Done and Cancelled (all of them, or those finished 7 or 30+ days ago). With several projects in view it lists them (ticked when there are up to three, unticked when more): tick the ones to clean up, or use **All projects in view**. It archives every done and cancelled task of those projects; the assignee, label, priority and search filters don't apply. Archived tasks leave the board and lists but stay searchable, and you can unarchive them from the task.
  - **Trash**: the deleted tasks of the projects in view, most recently changed first, each with **Restore**, which puts it back in its column. Restoring needs the *Delete tasks* permission.

![The trash (Daywalker theme)](../images/trash-day.png)

Recurring, Schedules and Templates list everything across your workspaces, with a **Show** filter at the top to narrow them to one workspace or project. On a phone, **⋯ More** holds the Table link and these actions, and the archived toggle sits in the **Filters** fold.
