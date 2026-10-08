# Foreman user guide

Foreman runs teams of Claude Code agents on your own machine. You describe what you want and a lead
agent splits the work into tasks. The tasks go to the agents on its team, who do the work in your
project folders and report back. You watch everything live in the browser. You step in only when
an agent needs you.

Foreman is a single program that runs locally. Its interface opens in your browser at
`http://127.0.0.1:7777`. It is not a cloud service. Your code stays on your machine and is sent only
to Claude, the same way it is when you use Claude Code yourself.

## What you can do with it

- **Give a goal, get a team working on it.** A lead agent plans the work, creates tasks for its team,
  checks the results and sends work back when it is not good enough.
- **See every step.** A live board shows each task as it moves. For every task you can read the
  conversation between the agents, the file diffs, the commands that were run and who decided what.
- **Stay in control.** Agents ask you when they need a decision. Anything they were not allowed to do
  shows up as a request, and you approve or dismiss it in one click.
- **Let it run on its own.** With Full autonomy on, the lead decides in your place. Recurring tasks
  can run checks or maintenance on a schedule.

## Guide

1. [Getting started](getting-started.md): install, run, your first project
2. [Core concepts](concepts.md): projects, folders, agents, the team hierarchy, tasks
3. [Building your team](team.md): the canvas, the agent editor, models, tools, setting up with AI
4. [Working with tasks](tasks.md): the board, custom columns, the task window, recurring tasks
5. [Needs you and autonomy](needs-you.md): questions, access requests, stuck tasks, Full autonomy
6. [Linked projects](linked-projects.md): agents from different projects working together
7. [Permissions, safety and data](safety.md): what agents can and cannot do, where your data lives
8. [FAQ and troubleshooting](faq.md)
