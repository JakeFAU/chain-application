# Decision Records

Approved decisions with durable consequences are recorded here as
`NNNN-<short-slug>.md`, numbered sequentially from `0001`. A decision record
exists so that "explicitly approved" has a paper trail: future work can see
what was decided, which alternatives were considered, and what the decision
constrains.

Record a decision here when it resolves a Stop-and-Escalate item from this
repository's `AGENTS.md` or the root `AGENTS.md` — ledger wire formats,
canonical serialization, hashing, event schemas, cryptographic primitives, key
custody, identity semantics, deletion/privacy semantics, breaking API changes,
and similar high-reversal-cost choices. Ordinary engineering decisions do not
need one.

Each record is short and contains:

1. **Context** — the problem and the constraints that matter;
2. **Decision** — what was decided, stated precisely enough to implement and
   test against;
3. **Alternatives** — what else was considered and why it was not chosen;
4. **Consequences** — what this constrains, enables, or defers, including
   replay/compatibility impact; and
5. **Status** — `proposed`, `accepted`, or `superseded` with a pointer to the
   superseding record.

Records are immutable once accepted. Changing a decision means writing a new
record that supersedes the old one, mirroring how the ledger itself treats
history.
