# Core concepts

## Project

A project is one product or piece of work, for example "Shop backend" or "Company landing page". It has:

- a **goal**: what "done" looks like;
- a **shared context** that every agent reads: stack, build and test commands, conventions, what is
  already decided and what is off-limits;
- one or more **folders** where the work happens;
- a **team** of agents;
- a **board** with its tasks.

Projects are independent of each other unless you [link them](linked-projects.md).

## Folders

A project has one or more folders, for example a client app and a landing page. Folders can be git
repositories or plain folders. Each folder has a name, a path and its own context (its stack, its
commands).

- Every agent has a **home folder**. It runs there and writes only there.
- It can **read** all the other folders of the project.
- The first folder is the default. An agent without a chosen folder works in it.

If a folder has a `CLAUDE.md`, agents read it like Claude Code always does. In the project
instructions you can also load it into the folder's context with **Load CLAUDE.md**.

## Agents

An agent is a Claude Code worker with:

- a **name** and a **role** (for example "Rook, Backend developer");
- a **model** (Opus, Sonnet, Haiku, Fable, as the latest version or a pinned one);
- **Takes**: the kinds of tasks it receives. Its lead assigns work by this;
- **background**: what it knows about the project;
- **instructions**: how it works and what it must never do;
- a **definition of done**: what it hands over when it finishes;
- **tools**: what it is allowed to do (see [Permissions](safety.md)).

Each agent keeps its own Claude session, so it remembers earlier work. If you change its instructions,
it starts a fresh session on its next run. **New chat** in an agent's chat also starts a fresh session.
The old messages stay saved under **Earlier messages**.

## The team is a hierarchy

On the **Team** canvas, every agent either reports to a boss or reports to **You**.

| Role | What it means |
|---|---|
| **Root** | Reports to you. You give it goals in its chat. |
| **Lead** | Has agents under it. It splits work into tasks *for its direct team only*, checks the results, and accepts them or sends them back. |
| **Sub-lead** | A lead that also has a boss. It splits the task it received into sub-tasks, and reports up when they are all done. |
| **Worker** | Has a boss and no team. It does the task and reports it done. |
| **Reviewer** | Optional, per agent. Work from an agent with a reviewer goes to the reviewer first, then to the lead. |
| **Detached** | Not linked to anyone. It does not work or receive messages until you link it on the canvas. |

These roles are not set by hand. They follow from the links you draw. A project can have several
roots, for example two independent teams under you.

## Tasks

A task is a unit of work with a number (`T-12`), a title, a description and an assignee.

Without custom columns, a task moves through:

```
Queued -> In progress -> Review -> Done
```

1. A lead creates the task for one of its agents (or you create it from the board).
2. The agent works on it and calls *report done* with a summary.
3. If the agent has a reviewer, the reviewer approves or rejects it.
4. The lead that created the task **accepts** it, or **sends it back** with feedback.
5. After 3 send-backs or rejections, the task is **Blocked** and you decide.

Tasks you create yourself from the board come back to you for the final decision.

Follow-up work can be linked to an earlier task, so related tasks show as **one thread** on the board.

## Runs

Each time an agent works, HobStack starts a **run**: one headless Claude Code session. An agent has at
most one run at a time. **Parallel workers** in the project settings limits how many agents (other
than the ones reporting directly to you) run at the same time. **Budget per run** stops a run when it
reaches that cost. It only matters if you pay per use with an API key.

Next: [Building your team](team.md)
