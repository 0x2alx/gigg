# Import JSON and Copy as JSON

Copy an item as JSON in one place and import it in another, or paste JSON someone else prepared (a
colleague, an AI assistant) instead of typing it into a form.

## Import JSON

Each page that lists these things has an **Import JSON** button next to its **New** button:

| Where | Imports |
|---|---|
| **Schedules** (account menu) | schedules |
| **Templates** (account menu) | gig templates |
| **Recurring** (account menu) | recurring gigs, and the schedules and gig templates they use |
| **Import gigs** (account menu) | gigs |
| **Errands** page | errands and errand templates |

1. Paste the JSON, or choose a `.json` file. It may hold one object or a list, up to 100 objects and
   1 MB.
2. The preview lists one row per object: its kind, its name, and a place to pick where it goes (the
   workspace or project you were looking at to start with; **Shared** for schedules, gig templates and
   errand templates if you hold the Shared library permission). A row with a problem says so and is
   skipped. Fields gigg doesn't use are listed as ignored.
3. **Create** makes the good rows one by one (in dependency order) and shows each result: created, or the message the form
   would have given. The list behind the dialog refreshes.

Every object is made through the same checks as the form and the API: the permissions you hold, the
validation of each field. A row can pass the preview and still be refused (a cap, a permission, a time
that isn't one); it is then marked failed, and the others still go through.

## The format

An object is what the create call takes, plus a `kind`: `schedule`, `gig_template`, `errand_template`,
`gig`, `errand` or `recurring_gig`. A page only takes its own kinds; any other is refused in the
preview.

```json
[
  { "kind": "schedule", "name": "Weekday mornings", "freq": "weekly", "weekdays": [1, 2, 3, 4, 5], "window_start": "09:00", "window_minutes": 60 },
  { "kind": "gig_template", "name": "Weekly report", "title": "Write the weekly report", "labels": ["report"], "assignees": ["scout"] },
  { "kind": "recurring_gig", "template": "Weekly report", "schedule": "Weekday mornings" },
  { "kind": "gig", "title": "Check the backups", "priority": 2, "due_at": 1790000000000 }
]
```

- Ids, version, workspace, project, who made it and counts are ignored, so a copy imports cleanly
  anywhere. The place comes from the picker.
- People are named by username (`assignees`, `followers`), and a name that nobody has is flagged.
- A recurring gig names its template and schedule with `template` and `schedule` (resolved among the
  ones in the chosen workspace and the shared ones), or with `template_id` and `schedule_id`. When
  nothing matches, the row is skipped.
- One list can hold the schedules and templates and the recurring gigs that use them. The dialog creates
  them in order (schedules, then templates, then what uses them, then gigs), whatever order they were
  pasted in, and a name that matches a row of the same list shows "will be created first" in the
  preview. If that row is refused, the rows that use it fail with "schedule 'X' wasn't created".
- Gigs take what the new-gig form takes (title, description, priority, labels by name, dates, checklist
  items, subgigs, run settings). Comments, relations and the gig's parent are not carried.
- Custom field values are keyed by field id and only fit a workspace that has those fields.

## Copy as JSON

- A schedule, a template and a recurring gig have **Copy JSON** in their list.
- An errand has a copy button at the top of its panel; a shared errand template has **Copy JSON** in
  **Shared templates**.
- A gig has **Copy as JSON** in the menu (the gear) of its card.

The text goes to the clipboard in the format above, and imports as it is.
