# Working with tasks

## The board

The **Tasks** tab shows a live board. By default the columns are:

| Column | Meaning |
|---|---|
| Queued | Waiting for its agent to be free |
| In progress | An agent is working on it |
| Review | Reported done; waiting for the reviewer or the lead. The card shows who has it. |
| Done | Accepted |

Each card shows the task number and title, the agent, a short description and labels:

- **Blocked**: rejected or sent back 3 times. You decide (see below).
- **stuck**: no one has moved it for a while (see [Needs you](needs-you.md#stuck-tasks)).
- **N files**: how many files the work changed.
- **↻**: created by a recurring task.
- **From <project>**: requested by a [linked project](linked-projects.md).

When a project has several folders, chips above the board filter tasks by folder.

Follow-up tasks that continue earlier work are grouped with it, so a thread of related tasks shows as one
card.

## Creating tasks yourself

Usually you give goals in the root agent's chat and the leads create the tasks. You can also click
**+ Task** to create one directly: a title, a description and an agent. If the project has custom
columns, the task enters the first column instead.

A task you create comes back to **you** for the final decision. Open it and use **Accept**, or
**Send back** with feedback. With Full autonomy on, the root lead decides instead (and you still can).

## Custom columns (work stages)

Every project works differently. Click **Columns** to define your own stages, for example
`Analysis -> Dev -> QA`. Each stage has:

- a **name**;
- a **type**: **Work** (the agent does the work and reports it done) or **Check** (the agent approves,
  or rejects back to the previous work stage);
- the **agents** who work in it. The first free agent takes the next task.

A task moves to the next stage on its own when the current one finishes. After the last stage it goes
to **With lead**, where the lead that created it (or you, for tasks you created) accepts it or sends it
back. Then it goes to **Done**. Three rejections in a stage block the task.

Projects without custom columns use the default board, and each agent's own reviewer.

## The task window

Click a card to open the task. The page address points to the task, so you can bookmark it. Besides the description and feedback, it has four tabs:

| Tab | What you see |
|---|---|
| **Conversation** | How the agents coordinated: the lead's brief, what each agent said, reports, review feedback, questions to you and your answers. If an agent is working on it now, **Watch live** opens its live output. |
| **History** | A timeline of everything that happened: created, assigned, started, reported, approved, sent back, blocked... |
| **Changes** | Every file changed, as a colored diff per run, plus the commands that were run. Changes made through shell commands (for example `sed`) are listed as commands, not as diffs. |
| **Flow** | The whole delegation tree from the original request: who asked in chat, which lead created which task, how a sub-lead split it into sub-tasks, who checked it, who accepted it, and how long it took. |

**Show report** reveals the agent's full report. Actions appear when they apply:
**Accept / Send back**, **Accept / Unblock** for blocked tasks, **Assignee** for queued or blocked
tasks, and **Nudge**.

## Recurring tasks

Click **Recurring** to have tasks created on a schedule, for example "Weekdays at 09:00: run the tests
and report what is broken".

- **Schedule**: every N hours (1-24), daily at a time, or weekly on chosen days at a time. Times use your
  computer's local time.
- **Agent**: who gets the task, or the first free agent in the first column.
- **Accept automatically when done**: skips your Accept / Send back. Questions and blocked tasks still
  reach you.

Each occurrence is a normal task on the board. If the previous one is still open, the next one is
skipped. Recurring tasks only run while Foreman is running. If it was off at the scheduled time, one task
is created when it starts again (not one per missed time). As with any task, it waits in Queued until you
press **Start team**.

Use **Run now** to create one immediately, or the switch to turn a schedule off.

Next: [Needs you and autonomy](needs-you.md)
