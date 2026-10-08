# Needs you and autonomy

Agents work on their own, but some things only you can decide. Everything that waits for you is
collected in one place: the **Needs you** strip at the top of the project. The project's card on the
Projects page shows a badge such as **2 need you**.

## Questions

When an agent needs a decision, a permission or information that only you have, it asks you
explicitly. The question appears:

- in **Needs you**, with suggested answers as buttons when it is a choice;
- in that agent's chat, above the message box;
- as a **?** on the agent on the canvas.

Answer with one click or in your own words, or **Dismiss** it (the agent then decides itself). The
answer wakes the agent up. If it was working on a task, the task goes back to the queue and continues
with your answer.

## Access requests

When an agent tries something its tools don't allow, the attempt is refused and shows up in **Needs you**,
for example *Rook was denied Bash* with the command. Depending on the case you get:

- **Enable Shell for Rook**: gives the agent the Shell commands tool;
- **Allow package installs for Rook**: gives it the Install packages tool (for `npm install` and similar);
- **Open Rook**: go to the agent to look at it or change its tools yourself;
- **Dismiss**: leave things as they are.

The new tool applies from the agent's next run. Some actions are never allowed (see
[Permissions](safety.md)). For those you only get Open and Dismiss.

## Commands that need your OK

When an agent runs a command whose [rule](safety.md#command-rules) is **Ask** (for example `git push`), the
command is refused and you see *Rook wants to run* with the command. Choose:

- **Allow once**: the agent may run it, in its next run only;
- **Allow "git push" in this project**: adds an Allow rule for that command in this project;
- **Block**: adds a Block rule for it in this project;
- **Dismiss**: leave things as they are.

After an Allow, the agent is woken up and its task goes back to the queue, so it can try again. Full
autonomy never approves these for you.

## Stuck tasks

Foreman checks every minute for tasks that nobody is moving:

- a task waiting for a decision from an agent that is idle;
- a task **In progress** with nothing running (for example after a failure).

After 5 minutes without change, Foreman nudges the responsible agent once on its own. If the task is
still stuck 5 minutes later, it appears in **Needs you** with **Nudge again** and **Open <agent>**, and
its card gets a **stuck** label. Paused projects are not checked.

## Requests to add agents

When a lead wants to grow its team and you did not ask for it in chat, you see *"Lead wants to add
Quinn"* with the role, model, tools and reason. **Approve** or **Dismiss**. See
[Building your team](team.md#leads-can-add-agents-themselves).

## Work requests from linked projects

If another project asks this one for work, the request waits here with **Approve** / **Dismiss**. See
[Linked projects](linked-projects.md).

## Full autonomy

Turn on **Full autonomy** in **Project instructions** when you want the project to run without you. The
project then shows an **Autonomous** label.

With Full autonomy:

- agents don't ask you questions. They make reasonable decisions and state their assumptions in their
  reports;
- the **root lead** decides what you would have decided: it accepts or sends back the tasks you created
  from the board, and decides blocked tasks. A blocked task gets one more try, and if it blocks again it
  is accepted as it is. Nothing loops forever;
- leads can add agents to their team without your approval.

You still get **access requests** and **work requests from linked projects**, and you can still decide
any task yourself from the board.

The change applies from each agent's next run.
