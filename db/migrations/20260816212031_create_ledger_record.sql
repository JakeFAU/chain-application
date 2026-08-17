-- migrate:up
CREATE TABLE ledger_record (
    record_digest          bytea         NOT NULL,
    ledger_id              bytea         NOT NULL,
    sequence_number        numeric(20,0) NOT NULL,
    previous_record_digest bytea         NULL,
    event_kind             numeric(20,0) NOT NULL,
    payload_version        numeric(20,0) NOT NULL,
    record_bytes           bytea         NOT NULL,
    inserted_at            timestamptz   NOT NULL DEFAULT now(),

    CONSTRAINT ledger_record_digest_pk
        PRIMARY KEY (record_digest),
    CONSTRAINT ledger_record_ledger_sequence_unique
        UNIQUE (ledger_id, sequence_number),
    CONSTRAINT ledger_record_previous_fk
        FOREIGN KEY (previous_record_digest) REFERENCES ledger_record (record_digest),
    CONSTRAINT ledger_record_digest_len
        CHECK (octet_length(record_digest) = 32),
    CONSTRAINT ledger_record_ledger_id_len
        CHECK (octet_length(ledger_id) = 32),
    CONSTRAINT ledger_record_previous_len
        CHECK (previous_record_digest IS NULL OR octet_length(previous_record_digest) = 32),
    CONSTRAINT ledger_record_bytes_bound
        CHECK (octet_length(record_bytes) BETWEEN 1 AND 131200),
    CONSTRAINT ledger_record_sequence_range
        CHECK (sequence_number BETWEEN 0 AND 18446744073709551615),
    CONSTRAINT ledger_record_event_kind_range
        CHECK (event_kind BETWEEN 0 AND 18446744073709551615),
    CONSTRAINT ledger_record_payload_version_range
        CHECK (payload_version BETWEEN 0 AND 18446744073709551615)
);

CREATE FUNCTION ledger_record_append_only() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'ledger_record is append-only';
END;
$$;

CREATE TRIGGER ledger_record_no_mutate
    BEFORE UPDATE OR DELETE ON ledger_record
    FOR EACH ROW EXECUTE FUNCTION ledger_record_append_only();

CREATE TRIGGER ledger_record_no_truncate
    BEFORE TRUNCATE ON ledger_record
    FOR EACH STATEMENT EXECUTE FUNCTION ledger_record_append_only();

-- migrate:down
-- Policy: authoritative ledger tables have no automated destructive rollback.
-- ledger_record holds signed history that cannot be rebuilt from anywhere else.
-- Removing it is a Stop-and-Escalate operator decision with an explicit
-- preservation procedure, not a `dbmate rollback`. This block raises rather
-- than no-opping: a silent no-op would delete the schema_migrations row while
-- the table survived, so the next `dbmate up` would fail against an existing
-- table.
DO $$
BEGIN
    RAISE EXCEPTION 'ledger_record holds authoritative ledger history; rollback is an explicit operator decision';
END;
$$;
