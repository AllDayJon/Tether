# Tether Product Identity

## One-line definition

Tether watches a terminal session, keeps track of what happened, and gives that context to an agent like Claude Code when the user asks for help.

Tether does not replace the terminal or the agent. Its job is to make the handoff fast, accurate, and easy to control.

## Problem

Users do real work in the terminal before they ask for help.

They inspect logs, run commands, hit errors, retry things, SSH into machines, and test ideas. By the time they say "fix this," most agent tools start cold. The user has to explain the problem, the environment, and what they already tried.

Tether exists to close that gap.

## What Tether does

Tether:

- observes terminal activity in a Tether-backed session
- keeps relevant context from that session
- prepares a clean handoff to an external agent
- gives the user visibility and control over what happens next

The main value is not the agent loop itself. The main value is making the agent start with the right context.

## What Tether is not

Tether is not:

- an AI model
- a coding agent
- a replacement for Claude Code
- a replacement terminal
- an IDE
- a chat wrapper around shell output

Tether should not try to be a worse version of tools that already do those jobs well.

## What Tether owns

Tether owns:

- terminal session observation
- context capture and filtering
- summaries and continuity across turns
- handoff packaging for agents
- visibility into what the agent is doing
- execution controls and safety boundaries
- bring-your-own-terminal compatibility

## What Tether does not own

Tether does not own:

- the core planning and execution loop of the agent
- being the main coding assistant
- replacing the user's terminal workflow
- broad IDE features
- autonomy as the main product value

## Product principles

### Bring your own terminal

Tether works with the terminal the user already uses.

It should fit into existing workflows instead of asking users to switch to a new terminal.

### Start with the session, not the question

Tether begins collecting context when the user starts a Tether session, not when they ask for help.

That context should reduce how much the user has to explain later.

### Companion, not cockpit

Tether supports the operator. It does not take over the workflow.

The user should stay in control.

### Warm handoff over cold start

Tether's main job is to get an external agent up to speed quickly and accurately.

### Visible control

What Tether sends, what the agent is doing, and what can be executed should be clear to the user.

## Relationship to agents

Tether is agent-agnostic.

Claude Code is the first agent Tether works with, but Tether should be designed so other agents can be supported later.

Tether provides context, continuity, and control. The external agent provides the reasoning and execution loop.

## Feature test

Before building a feature, ask:

1. Does this improve context, continuity, control, or compatibility?
2. Does this reduce how much the user has to re-explain?
3. Does this fit a bring-your-own-terminal workflow?
4. Are we drifting into building our own worse agent, terminal, or IDE?

If the answer to 4 is yes, we should probably not build it.

## In bounds

Examples of good feature directions:

- better capture of command and output history
- better context selection for handoff
- clearer review of the handoff before agent launch
- better visibility into agent actions
- better command controls and safety rules
- support for more external agents through the same handoff model

## Out of bounds

Examples of bad feature directions:

- building our own general-purpose coding agent
- replacing the user's terminal emulator
- adding IDE-style workspace features as a core product area
- copying features whose value is only that another agent tool already has them

## Product promise

Keep your terminal and workflow. When you need help, the agent should already be up to speed.

