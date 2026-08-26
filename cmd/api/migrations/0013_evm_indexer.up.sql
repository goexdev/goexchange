-- EVM indexer state
-- Tracks last indexed block for each chain
CREATE TABLE evm_indexer_state (
    chain_id        TEXT PRIMARY KEY,
    last_block      BIGINT NOT NULL DEFAULT 0,
    hot_wallet      TEXT NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- EVM indexed transfers log
-- All transfers detected by the indexer (for reconciliation)
CREATE TABLE evm_indexed_transfers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chain_id        TEXT NOT NULL,
    tx_hash         TEXT NOT NULL,
    log_index       INT NOT NULL,
    block_number    BIGINT NOT NULL,
    token_address   TEXT NOT NULL,
    from_address    TEXT NOT NULL,
    to_address      TEXT NOT NULL,
    amount          NUMERIC(38,18) NOT NULL,
    asset           TEXT NOT NULL,
    processed       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chain_id, tx_hash, log_index)
);

CREATE INDEX idx_evm_transfers_chain_block ON evm_indexed_transfers (chain_id, block_number);
CREATE INDEX idx_evm_transfers_unprocessed ON evm_indexed_transfers (processed) WHERE NOT processed;
