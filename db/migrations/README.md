# Database migrations

No application schema exists yet. This directory is reserved for future
dbmate migration files, which use timestamp-prefixed filenames.

Schema design, including any ledger, projection, or application table, requires
its own approved task. Relational migration history is not ledger replay:
migrations evolve a disposable relational representation, while deterministic
replay reconstructs ledger-derived state from accepted, ordered ledger events.
