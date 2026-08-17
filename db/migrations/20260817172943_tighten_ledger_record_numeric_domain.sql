-- migrate:up
ALTER TABLE ledger_record
    DROP CONSTRAINT ledger_record_sequence_range,
    DROP CONSTRAINT ledger_record_event_kind_range,
    DROP CONSTRAINT ledger_record_payload_version_range;

ALTER TABLE ledger_record
    ADD CONSTRAINT ledger_record_sequence_range
        CHECK (sequence_number BETWEEN 1 AND 18446744073709551615),
    ADD CONSTRAINT ledger_record_event_kind_range
        CHECK (event_kind BETWEEN 1 AND 18446744073709551615),
    ADD CONSTRAINT ledger_record_payload_version_range
        CHECK (payload_version BETWEEN 1 AND 18446744073709551615);

-- migrate:down
-- Policy: authoritative ledger tables have no automated destructive rollback.
-- ledger_record holds signed history that cannot be rebuilt from anywhere else.
-- Loosening or removing these range constraints is a Stop-and-Escalate operator
-- decision with an explicit preservation procedure, not a `dbmate rollback`.
-- This block raises rather than no-opping: a silent no-op would delete the
-- schema_migrations row while the tightened constraints survived, so the next
-- `dbmate up` would fail by trying to add constraints that already exist.
DO $$
BEGIN
    RAISE EXCEPTION 'ledger_record holds authoritative ledger history; rollback is an explicit operator decision';
END;
$$;
