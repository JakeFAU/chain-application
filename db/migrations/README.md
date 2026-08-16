# Database migrations

No application schema exists yet. This directory is reserved for future
dbmate migration files, which use timestamp-prefixed filenames.

Relational migration history is not ledger replay: migrations evolve a
disposable relational representation, while deterministic replay reconstructs
ledger-derived state from accepted, ordered ledger events.

Authoritative ledger migrations do not provide automated destructive down
migrations. Their down block is present and raises an exception carrying that
policy, so an operator sees deliberate policy rather than a missing half of the
file. Schema rollback that would remove or mutate ledger history is an explicit
Stop-and-Escalate operator decision. Rebuildable projections retain conventional
reversible migrations, including `DROP TABLE` on down, because the ledger can
reconstruct them.
