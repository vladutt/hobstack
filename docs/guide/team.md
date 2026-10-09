# Building your team

## The Team tab

The **Team** tab shows your team as a graph. **You** sit at the top, and every agent is a card with its
role, what it takes and a live status dot (Working, Queued or Idle). Use **Full screen** for more room.

| To... | Do this |
|---|---|
| Move an agent | Drag its card. The position is saved. |
| Create an agent under another | Drag from the ● at the bottom of a card onto empty space. |
| Create a detached agent | Double-click empty space on the canvas. Link it later. |
| Open an agent | Click its card. The right panel shows its **Details** and **Chat**. |
| Link two agents | Drag from the ● at the bottom of a card (or of **You**) onto another card, then choose **Delegate to X** or **Review work of X**. |
| Remove a link | Click the × on the line (or select the line and press Delete). |

- **Delegate to** makes the target report to the source. Dragging from **You** makes the target a root.
- **Review work of** makes the source the target's reviewer. Review links are drawn as dashed amber lines.
- Deleting a delegation link leaves the target detached until you link it again.

## The agent editor

| Field | What it is for |
|---|---|
| Name | Short and unique in the project. The lead assigns tasks by this name. |
| Role | For example "Backend developer". |
| Folder | Where the agent works and writes, when the project has several folders. |
| Model | **Always latest** (for example *Opus (latest)*) follows new versions. **Pinned version** stays on an exact one. |
| Takes (responsibilities) | The kinds of tasks this agent receives. Leads assign work by it. |
| Reports to / Reviewed by | The same links as on the canvas. |
| Background | Who it is and what it knows about the project. |
| Instructions | How it works and what it must never do. |
| Definition of done | What it hands to its boss: summary, changed files, how to verify. A root agent's version is the final summary it reports to you. |
| Tools | What it is allowed to do (see below). |

**Compiled prompt** shows the exact instructions the agent will receive, updated as you type.

### Tools

| Tool | Allows |
|---|---|
| *(always)* | Reading and searching files, and the team tools (tasks, reports, questions) |
| **Write files** | Creating and editing files, only inside the agent's own folder |
| **Shell commands** | Running terminal commands (build, tests, scripts...) |
| **Install packages (npm, composer, pip, go get)** | `npm install`, `composer require`, `pip install`, `go get`, even without Shell |
| **Git: status, diff, commit, branch** | Those git commands only, without full shell access. `git push` is never allowed. |
| **Web search** | Searching the web and fetching pages |

Give agents only what their role needs. See [Permissions, safety and data](safety.md) for the details
and the limits.

## Setting up agents with AI

### For a new project

Use **New project with AI** on the Projects page (see [Getting started](getting-started.md)).

### For a folder of an existing project

When you add a folder to a project (for example a new landing page next to the app), open
**Project instructions** and click **Set up agents with AI** on that folder. Save the folder first.
The analyst reads the folder and sees your current team. It then proposes:

- the folder's context;
- a small team for that folder (usually a sub-lead and 1-3 workers) under one of your existing agents.

Your existing agents are never changed. Review the proposal, adjust it (or ask the HobStack panel), then
click **Add agents**.

## Leads can add agents themselves

A lead or root agent can add an agent to its own team. It does this when no current member fits part
of the work, or to run independent sub-tasks in parallel. The rules are enforced by HobStack:

- the new agent always goes directly under the lead that adds it, in the lead's folder;
- it can only get tools the lead has itself;
- a lead can have at most 6 agents it added (or asked for) at a time.

**Who approves:**

- If you asked for it in the lead's chat ("add a QA agent"), or the project has Full autonomy on, the
  agent is added right away.
- Otherwise the request appears in **Needs you** as *"Lead wants to add Quinn"*, with the role, tools
  and reason. You **Approve** or **Dismiss** it.

Agents added this way show **Added by <lead>**. When the work is done, the lead can remove them. They
stay on the canvas, detached, with their history.

## Talking to agents

Every agent has a chat. Use the root agent's chat to give goals. You can also message any agent
directly, for example to correct a worker. Its boss is copied on what you write, so the lead
stays informed. Attach files with the paperclip, paste images, or drag files
onto the chat. Agents can read images and PDFs.

Files you add in the project's **Files** tab are visible to the whole team.

Next: [Working with tasks](tasks.md)
