package store

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version  INTEGER PRIMARY KEY,
  applied  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tickets (
  id             TEXT PRIMARY KEY,
  title          TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  type           TEXT NOT NULL DEFAULT 'ticket'
                 CHECK(type IN ('ticket')),
  status         TEXT NOT NULL DEFAULT 'draft'
                 CHECK(status IN ('draft','ready','in_progress','in_review','approved','merged')),
  feature_branch TEXT NOT NULL DEFAULT '',
  worktree_path  TEXT,
  repo_path      TEXT,
  created        INTEGER NOT NULL,
  updated        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS config (
  key    TEXT PRIMARY KEY,
  value  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tasks (
  id                TEXT PRIMARY KEY,
  ticket_id         TEXT NOT NULL,
  title             TEXT NOT NULL,
  description       TEXT NOT NULL DEFAULT '',
  position          INTEGER NOT NULL,
  commit_hash       TEXT,
  verifiable_result TEXT NOT NULL DEFAULT '',
  completed_at      INTEGER,
  created           INTEGER NOT NULL,
  updated           INTEGER NOT NULL,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS blocked_by (
  ticket_id   TEXT NOT NULL,
  blocker_id  TEXT NOT NULL,
  PRIMARY KEY (ticket_id, blocker_id),
  FOREIGN KEY (ticket_id)  REFERENCES tickets(id) ON DELETE CASCADE,
  FOREIGN KEY (blocker_id) REFERENCES tickets(id) ON DELETE CASCADE,
  CHECK (ticket_id != blocker_id)
);

CREATE TABLE IF NOT EXISTS comment_threads (
  id       TEXT PRIMARY KEY,
  task_id  TEXT NOT NULL,
  status   TEXT NOT NULL DEFAULT 'open'
           CHECK(status IN ('open','needs_attention','resolved')),
  created  INTEGER NOT NULL,
  FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS thread_messages (
  id        TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL,
  author    TEXT NOT NULL,
  text      TEXT NOT NULL,
  created   INTEGER NOT NULL,
  FOREIGN KEY (thread_id) REFERENCES comment_threads(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS notes (
  id        TEXT PRIMARY KEY,
  ticket_id TEXT NOT NULL,
  author    TEXT NOT NULL,
  text      TEXT NOT NULL,
  created   INTEGER NOT NULL,
  FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tickets_status         ON tickets(status);
CREATE INDEX IF NOT EXISTS idx_tickets_type           ON tickets(type);
CREATE INDEX IF NOT EXISTS idx_tasks_ticket           ON tasks(ticket_id);
CREATE INDEX IF NOT EXISTS idx_tasks_position         ON tasks(ticket_id, position);
CREATE INDEX IF NOT EXISTS idx_blocked_by_blocker     ON blocked_by(blocker_id);
CREATE INDEX IF NOT EXISTS idx_threads_task           ON comment_threads(task_id);
CREATE INDEX IF NOT EXISTS idx_thread_messages_thread ON thread_messages(thread_id);
CREATE INDEX IF NOT EXISTS idx_notes_ticket           ON notes(ticket_id);
`
