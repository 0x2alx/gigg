# Inbox and notifications

## Inbox

The **Inbox** lists every task in a Waiting status across the workspaces you can see, oldest question first: usually an agent asked a question and needs a person. Each row shows the latest question and who asked it. Open the task, answer in a comment, then move it back to To-Do, or use **Resume** so the agent continues where it stopped.

![The inbox (Daywalker theme)](../images/inbox-day.png)

The envelope in the top-right corner of every page opens the inbox. Its badge shows how many tasks are waiting.

## Notifications

The bell in the top-right corner collects what happened to you. Click it to open the list under it (on a phone it opens the notifications page):

| Kind | When |
|---|---|
| mention | Someone wrote `@you` in a task description or a comment |
| assigned | You were assigned a task |
| question | Someone asked a question on a task you created or are assigned to |
| comment | Someone posted a report on a task you created or are assigned to |
| missed | A task of yours wasn't started before its window closed |

Click one to open the task. **Mark all read** clears the badge. The command palette (**Ctrl+K**) also has a **Notifications** page with the full list.

## Live updates

The small dot in the top-right corner, beside the envelope, shows the live connection to the server (hover it for the details):

- **Live** (green): changes by others, people or agents, appear within a second or two.
- **Connecting** (amber): reconnecting after a restart or a network blip.
- **Offline** (grey): no connection, still retrying. Pages work, but only update when you act or reload.
