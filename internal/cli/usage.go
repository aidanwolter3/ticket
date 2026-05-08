package cli

import "fmt"

func PrintUsage() {
	fmt.Print(`ticket — local-first ticket tracker

Usage:
  ticket [--db path]                          launch TUI
  ticket draft [--db path] --title STR --repo STR [--description STR|-] [--branch STR] [--json]
                                              create a draft ticket from flags; outputs assigned ID (or --json for full ticket)
  ticket import [--db path] [file]            batch-create tickets from JSON (stdin if no file)
  ticket ls [--db path] [--status s] [--json] list tickets
  ticket get [--db path] <id>                 get a single ticket as JSON (includes tasks and threads)
  ticket claim-work [--db path] [--json]      atomically claim the next available work item
  ticket peek-work [--db path] [--json]       view claimable work without claiming
  ticket transition [--db path] <id> <status> <author>
                                              transition a ticket's status
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
  ticket task complete [--db path] <task-id>  mark a task complete
  ticket task uncomplete [--db path] <task-id>
                                              mark a task incomplete
  ticket block [--db path] <ticket-id> <blocker-id>
                                              record that <ticket-id> is blocked by <blocker-id>
  ticket unblock [--db path] <ticket-id> <blocker-id>
                                              remove that dependency
  ticket ready [--db path] <ticket-id> <author>
                                              promote a draft ticket to ready
  ticket redraft [--db path] <ticket-id> <author>
                                              destroy worktree+branch and move ticket back to draft (human only)

  ticket review-submit [--db path] <id> <author>
                                              flip all active threads→ready and ticket→ready (human only; requires ≥1 active thread)
  ticket approve [--db path] <id> <author>    approve an in_review ticket (human only)
  ticket merge [--db path] <id> <author>      ff-merge, delete branch, remove worktree (human only)
  ticket config set [--db path] <key> <value> set a config value
  ticket config get [--db path] <key>         get a config value (worktrees defaults to true)
  ticket config ls  [--db path]               list all config settings with defaults
  ticket delete [--db path] <id>              delete a ticket
  ticket purge [--db path] --yes              delete the database file
  ticket agent clear [--db path] <ticket-id>  remove all agent sessions for a ticket

`)
}
