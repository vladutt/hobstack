# Linked projects

Sometimes two projects depend on each other, for example a *Frontend* and a *Backend*. Link them and
their agents can work together.

## Linking

Open a project, go to **Linked projects** and choose a project in **Link a project...**. A link works
both ways: each project can ask the other. Only you can create or remove links; agents cannot.

## What agents can do across projects

**Ask a question.** Any agent can ask a linked project something, for example "what does /users return?".
The question goes to that project's **root agent**, which answers it. The answer comes back to the agent
that asked, as a message. Each agent can have at most 3 open questions to other projects. An agent that
is answering a question from another project cannot ask a new one back, so projects can't keep asking
each other in circles.

**Request work.** Leads and root agents can ask a linked project to do something, for example "add the
/export endpoint". This becomes a normal task for the other project's root, on that project's board,
labelled **From Frontend**. The other project handles it with its own team, stages and reviewers. When
it is done, the report goes back to the agent that asked.

**Who approves work requests:**

- If you asked for it yourself in the lead's chat, the request goes through right away.
- Otherwise it waits in **Needs you on the target project** ("Frontend (Lead) asks for work: ..."), with
  **Approve** / **Dismiss**. Full autonomy does not skip this approval: you always choose what work
  enters another project.

The other project is treated as a black box: requests only reach its root, and its own rules stay in
place. If the target project is paused, questions and tasks wait until you start its team.

## Removing a link

Open questions between the two projects are closed, and pending work requests are dismissed. Both
sides get a note about it. Work that was already approved continues, and its report still comes back.
