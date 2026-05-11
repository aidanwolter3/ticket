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
  ticket get [--db path] <id>                 get a single ticket as JSON (includes tasks and threads)
  ticket update [--db path] <id> [--title <title>] [--description <desc>]
                                              update a ticket's title or description
  ticket note add [--db path] <ticket-id> <author> <text>
                                              add a note to a ticket
  ticket thread reply [--db path] <thread-id> <author> <text>
                                              add a reply to a thread
  ticket thread transition [--db path] <thread-id> <new-status> <author>
                                              transition a thread's status (agents: ready→active only)
  ticket task add [--db path] <ticket-id> --title <title> [--description <desc>] [--verifiable-result <vr>]
                                              add a new task to a ticket
  ticket task ls [--db path] [--json] <ticket-id>
                                              list tasks for a ticket
  ticket task complete [--db path] [--most-recent-commit] <task-id>
                                              mark a task complete; --most-recent-commit resolves HEAD from repo_path
  ticket task uncomplete [--db path] <task-id>
                                              mark a task incomplete
  ticket task delete [--db path] <task-id>   delete a task
  ticket task update [--db path] <task-id> [--title <title>] [--description <desc>] [--verifiable-result <vr>]
                                              update a task's fields
  ticket task move [--db path] <task-id> <position>
                                              move a task to a new position
  ticket block [--db path] <ticket-id> <blocker-id>
                                              record that <ticket-id> is blocked by <blocker-id>
  ticket unblock [--db path] <ticket-id> <blocker-id>
                                              remove that dependency
  ticket ready [--db path] <ticket-id> <author>
                                              promote a draft ticket to ready
  ticket in-progress [--db path] <ticket-id> <author>
                                              move a ticket from ready to in_progress (agent-facing)
  ticket in-review [--db path] <ticket-id> <author>
                                              move a ticket from in_progress to in_review (agent-facing)
  ticket redraft [--db path] <ticket-id> <author>
                                              destroy worktree+branch and move ticket back to draft (human only)

  ticket review-submit [--db path] <id> <author>
                                              flip all active threads→ready and ticket→ready (human only; requires ≥1 active thread)
  ticket config set [--db path] <key> <value> set a config value
  ticket config get [--db path] <key>         get a config value (worktrees defaults to true)
  ticket config ls  [--db path]               list all config settings with defaults
  ticket delete [--db path] <id>              delete a ticket
  ticket purge [--db path] --yes              delete the database file
  ticket agent clear [--db path] <ticket-id>  remove all agent sessions for a ticket

`)
}
