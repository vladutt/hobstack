# FAQ and troubleshooting

### Nothing happens after I write to an agent.

Check that the team is started: projects start paused every time Foreman starts. Press **Start team**.
Also check that the agent is linked on the canvas: a **detached** agent does not work or receive
messages.

### An agent says a command is blocked, but it has Shell commands.

Some commands are blocked for every agent: `git push`, `rm -rf`, `sudo`, `brew install`. Package
installs (`npm install`, `composer require`, `pip install`, `go get`) are blocked unless the agent has
**Install packages**. The refusal appears in **Needs you**. For package installs, click **Allow package
installs for <agent>**. For the others, run the command yourself.

### Runs fail with "You've hit your session limit".

Your Claude usage limit has been reached. The message says when it resets. Nothing is lost: the tasks
stay on the board. After the reset, write to the root agent or use **Nudge** on the affected tasks to
continue.

### A task has been sitting in Review for a long time.

Foreman nudges the responsible agent after 5 minutes. If it is still stuck, it shows in **Needs you**
with **Nudge again**. If the task is waiting for *you* (you created it, or it is blocked), open it and
choose **Accept** or **Send back**.

### The agent forgot what we talked about.

Changing an agent's instructions, links or folder starts a fresh session on its next run. So does
**New chat**. Its earlier messages are still saved under **Earlier messages**, and every run gets the
project's goal and shared context again.

### Can I change the team while it is working?

Yes. Changes apply from each agent's next run. An agent that is running finishes its current run with
the old settings.

### What happens to running work when I close Foreman?

Running agents are stopped. Their tasks go back to **Queued** and continue when you start the team again.
Tasks waiting for your answer stay where they are.

### Do agents see each other's work?

Every agent can read all the folders of its project and the project's files. Leads see their team's
reports. Agents don't read each other's chats.

### Can agents push to git or deploy?

`git push` is always blocked, and Foreman has no deploy step: publishing is left to you. Keep in mind that
an agent with **Shell commands** can get around the command blocks (see [Permissions](safety.md#limits-you-should-know)),
so give Shell only to agents you trust.

### Where are the logs?

Each task window shows the conversation, the history and the changes. The raw log of every run is in
`~/.foreman/logs/runs/`.
