# Permissions, safety and data

## How permissions work

Agents get their permissions only from Foreman, from the tools you tick on each agent. An agent that
tries something it is not allowed to do is refused immediately. The refusal does not block the run, and
it appears in [Needs you](needs-you.md#access-requests) so you can decide.

| Tool | Allows |
|---|---|
| *(always)* | Reading and searching files in the project's folders and attached files; the team tools |
| **Write files** | Creating and editing files, only in the agent's own folder |
| **Shell commands** | Terminal commands, following the [command rules](#command-rules) |
| **Install packages** | `npm install`, `composer require`, `pip install`, `go get` |
| **Git** | `git status`, `diff`, `log`, `add`, `commit`, `checkout -b` |
| **Web search** | Web search and fetching pages |

### Command rules

Command rules decide what an agent with **Shell commands** may run in the terminal. Each rule is a
command prefix with one of three modes:

| Mode | What happens |
|---|---|
| **Allow** | The command runs. |
| **Ask** | The command is refused and shows up in [Needs you](needs-you.md#commands-that-need-your-ok), where you can allow it. |
| **Block** | The command is refused, and the agent is told in its instructions not to try it. |

Default rules:

| Command | Default |
|---|---|
| `git push` | Ask |
| `rm -rf` | Ask |
| `brew install` | Ask |
| `sudo` | Block |

Where to change them:

- **Settings** (top bar): the defaults for all projects.
- **Project settings > Commands**: for one project, set each default to Inherit, Allow, Ask or Block, or add
  commands that exist only in that project.

How matching works:

- A rule matches how a command starts. The longest matching prefix wins, so `git push --force` can be
  Block while `git push` is Ask.
- Commands joined with `&&`, `;`, `|` or on separate lines, and commands inside `$( )`, are checked part
  by part.
- A longer Allow under a shorter Ask or Block cannot work, because the shorter rule still stops the
  command. Foreman shows a warning when you set this up.

Other things to know:

- Allow only matters for agents with Shell. Ask and Block also stop the Git tool's commands (for example git commit = Block).
- Claude Code's own internal task lists and sub-agents stay blocked for every agent, and cannot be
  changed. Agents coordinate through Foreman instead.
- Package installs (`npm install`, `composer require`, `pip install`, `go get`) are not covered by the rules:
  they stay refused unless the agent has **Install packages**.

### Limits you should know

- **Shell is powerful.** The rules match how a command starts. An agent with Shell commands can
  get around them (for example `rm -fr`, or a command wrapped in `bash -c`, which the rules do not look inside), and it can also write
  outside its own folder through the shell. Give Shell only to agents you trust with your machine.
- **Shell can reach Foreman itself.** An agent with Shell can call Foreman's local API, so it could change these rules. Give Shell only to agents you trust.
- **Your global Claude settings apply.** Rules in your `~/.claude/settings.json` (for example a global
  `permissions.allow`) also apply to agents and can widen what they can do. A project's own
  `.claude/settings.json` is ignored, but its `CLAUDE.md` is read.

## The setup analyst

The AI that analyses folders in the setup wizards can only read. It cannot write, run commands or browse
the web.

## Network and access

- Foreman listens only on your computer (`127.0.0.1`). Other machines on the network can't reach it.
- There are no user accounts. Anyone who can use your computer can use Foreman.
- The interface accepts requests only from `localhost`. This protects it from web pages that might try
  to send commands to it.

## Costs

Agents use your Claude Code login. With a subscription you don't pay per run, but agents do use your
usage limits. If the limit is reached, runs fail with a message saying when it resets. With an API key,
you can set a **Budget per run** in the project settings: a run stops once it reaches it. The limit is
checked between steps, so a run can go slightly over.

Every project starts paused when Foreman starts, so nothing runs until you press **Start team**.

## Where your data lives

Everything stays on your computer:

| Path | Contents |
|---|---|
| `~/.foreman/foreman.db` | Projects, agents, tasks, messages, history (SQLite) |
| `~/.foreman/logs/runs/` | The raw log of each agent run |
| `~/.foreman/projects/<id>/files/` | Files attached to a project or to chat messages |

Your project folders are never moved or deleted by Foreman, not even when you delete a project. What
agents send to Claude (code, files, messages) follows the same terms as using Claude Code directly.
