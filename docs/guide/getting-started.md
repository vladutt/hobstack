# Getting started

## Requirements

- **Claude Code** installed and logged in: the `claude` command must work in your terminal. HobStack
  runs every agent through it, so your Claude subscription or API key is what the agents use.
- macOS, Linux or Windows.

## Run HobStack

HobStack is a single executable file named `hobstack` (`hobstack.exe` on Windows). Put it somewhere in
your PATH, or run it from its folder as `./hobstack`. Start it from a terminal:

```
hobstack start
```

Your browser opens at `http://127.0.0.1:7777`. Options:

| Option | What it does |
|---|---|
| `--port 8080` | Listen on another port |
| `--no-open` | Don't open the browser automatically |

HobStack runs as long as the terminal command runs. Stop it with Ctrl+C. Agents that are working are
stopped too, and their tasks are put back in the queue.

> **Every project starts paused.** When HobStack starts, no agent runs (or costs anything) until you
> press **Start team** on a project. This is on purpose: you choose when work begins.

## Your first project

On the **Projects** page you have two ways to start.

### Option A: New project with AI (recommended)

1. Click **New project with AI**.
2. Pick one or more folders (optional) and describe what you want to do. If you don't pick a folder,
   or the folder is empty, you must write a description.
3. Choose the model for the analyst and click **Analyze**.
4. A read-only AI analyst reads your folder: the structure, the README, the dependency and build
   files, and a few representative files. Then it proposes:
   - the project's name, goal and shared context (stack, build and test commands, conventions);
   - a team: names, roles, what each agent takes, who reports to whom, reviewers and instructions.
5. Review the proposal. Everything is editable, and you can untick agents you don't want. You can
   also ask the **HobStack** panel on the right for changes in plain language ("add a QA agent", "make
   the lead use Opus"). Every change it makes can be undone with **Undo**.
6. Click **Create project**.

The analysis runs in the background. You can leave the page: it appears on Projects as a card
(Analyzing, Ready or Failed) that you can reopen.

> The analyst can only read. It never changes your files. It can propose only the *Write files* and
> *Git* tools for agents. Shell, web and package installs are tools you give yourself.

### Option B: New project (manual)

1. Click **New project**.
2. Fill in the brief: name, goal, shared context and folders. If you leave the folder empty, HobStack
   creates `~/HobStack/<project-name>` for you.
3. You get a default lead. Add more agents from the project's **Team** tab (see
   [Building your team](team.md)).

## Start working

1. Open the project and press **Start team**.
2. Select the root agent (the one linked to **You** on the canvas) and write your goal in its chat.
3. Watch the **Tasks** tab: the lead creates tasks, agents pick them up, and the work moves across
   the board.

To pause everything, press **Stop team**. Running agents are stopped and their tasks go back to the
queue. Nothing is lost.

Next: [Core concepts](concepts.md)
