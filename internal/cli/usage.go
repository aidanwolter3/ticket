package cli

import "fmt"

func PrintUsage() {
	fmt.Print(`ticket — local-first ticket tracker

Usage:
  ticket [--db path]                          launch TUI
  ticket draft [--db path] --title STR --repo STR [--description STR|-] [--json]
                                              create a draft ticket from flags; outputs assigned ID (or --json for full ticket)
  ticket import [--db path] [file]            batch-create tickets from JSON (stdin if no file)
  ticket ls [--db path] [--status s] [--json] list tickets
  ticket get [--db path] [--json] <id>        get a single ticket (--json for machine-readable output)
  ticket update [--db path] <id> [--title <title>] [--description <desc>]
                                              update a ticket's title or description
  ticket note add [--db path] <ticket-id> <author> <text>
                                              add a note to a ticket
  ticket thread reply [--db path] <thread-id> <author> <text>
                                              add a reply to a thread
  ticket thread transition [--db path] <thread-id> <new-status> [author]
                                              transition a thread's status
  ticket task add [--db path] <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>]
                                              add a new task to a ticket
  ticket task ls [--db path] [--json] <ticket-id>
                                              list tasks for a ticket
  ticket task get [--db path] [--json] <task-id>
                                              get a task
  ticket task update [--db path] <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>]
                                              update a task's fields
  ticket task move [--db path] <task-id> <position>
                                              move a task to a new position
  ticket task delete [--db path] <task-id>   delete a task
  ticket block [--db path] <ticket-id> <blocker-id>
                                              record that <ticket-id> is blocked by <blocker-id>
  ticket unblock [--db path] <ticket-id> <blocker-id>
                                              remove that dependency
  ticket ready [--db path] <ticket-id> <author>
                                              promote a draft ticket to ready
  ticket redraft [--db path] <ticket-id> <author>
                                              destroy worktree+branch and move ticket back to draft (human only)
  ticket review-submit [--db path] <id> <author>
                                              flip all active threads→needs_attention and ticket→ready (human only)
  ticket config set [--db path] <key> <value> set a config value
  ticket config get [--db path] <key>         get a config value (worktrees defaults to true)
  ticket config ls  [--db path]               list all config settings with defaults
  ticket delete [--db path] <id>              delete a ticket
  ticket purge [--db path] --yes              delete the database file
  ticket agent clear [--db path] <ticket-id>  remove all agent sessions for a ticket

  ticket --agent --help                       show agent-facing commands

`)
}

func PrintAgentUsage() {
	fmt.Print(`ticket --agent — agent-facing command surface

Usage:
  ticket --agent in-progress [--db path] <ticket-id>
                                              transition ready → in_progress
  ticket --agent in-review [--db path] <ticket-id>
                                              transition in_progress → in_review (hand off for review)
  ticket --agent task complete [--db path] [--most-recent-commit | --commit <hash>] <task-id>
                                              mark a task complete; records commit hash
  ticket --agent task uncomplete [--db path] <task-id>
                                              reverse a task completion
  ticket --agent task set-commit [--db path] <task-id> <hash>
                                              update the commit hash stored on a task (use after autosquash rebase)

Shared commands (also available without --agent):
  ticket get, ticket ls, ticket note add, ticket thread reply,
  ticket thread transition, ticket task ls, ticket task get

`)
}
