CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ---------------- TABLES -----------------
-- CREATE TABLE candles_1m
-- (
--     symbol           VARCHAR(20)    NOT NULL,
--     start_time       TIMESTAMPTZ    NOT NULL,
--     end_time         TIMESTAMPTZ    NOT NULL,
--     open             NUMERIC(32, 8) NOT NULL,
--     high             NUMERIC(32, 8) NOT NULL,
--     low              NUMERIC(32, 8) NOT NULL,
--     close            NUMERIC(32, 8) NOT NULL,
--     volume           NUMERIC(32, 8) NOT NULL,
--     quote_volume     NUMERIC(32, 8) NOT NULL,
--     taker_buy_volume NUMERIC(32, 8) NOT NULL,
--     number_of_trades BIGINT         NOT NULL,
--
--     UNIQUE (symbol, start_time)
-- );
--
-- -- Chunk candles by day
-- SELECT create_hypertable('candles_1m', 'start_time', chunk_time_interval => INTERVAL '1 day');
--
-- ---------------- INDEXES -----------------
-- CREATE INDEX idx_symbol_time ON candles_1m (symbol, start_time DESC);

CREATE TABLE exchanges
(
    code       VARCHAR(20) PRIMARY KEY,
    name       VARCHAR(50) NOT NULL,
    status     VARCHAR(20) DEFAULT 'ACTIVE',
    created_at TIMESTAMP   DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE exchange_symbols
(
    id            SERIAL PRIMARY KEY,
    exchange_code VARCHAR(20) REFERENCES exchanges (code),
    symbol        VARCHAR(50) NOT NULL,
    base_coin     VARCHAR(20),
    quote_coin    VARCHAR(20),
    status        VARCHAR(20) DEFAULT 'TRADING',
    updated_at    TIMESTAMP   DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (exchange_code, symbol)
);

---------------- TABLES -----------------
CREATE TABLE candles_1m
(
    exchange         VARCHAR(20)    NOT NULL,
    symbol           VARCHAR(20)    NOT NULL,
    start_time       TIMESTAMPTZ    NOT NULL,
    end_time         TIMESTAMPTZ    NOT NULL,
    open             NUMERIC(32, 8) NOT NULL,
    high             NUMERIC(32, 8) NOT NULL,
    low              NUMERIC(32, 8) NOT NULL,
    close            NUMERIC(32, 8) NOT NULL,
    volume           NUMERIC(32, 8) NOT NULL,
    quote_volume     NUMERIC(32, 8) NOT NULL,
    taker_buy_volume NUMERIC(32, 8) NOT NULL,
    number_of_trades BIGINT         NOT NULL,

    UNIQUE (exchange, symbol, start_time)
);

-- Chunk candles by day
SELECT create_hypertable('candles_1m', 'start_time', chunk_time_interval => INTERVAL '1 day');

---------------- INDEXES -----------------
CREATE INDEX idx_symbol_time ON candles_1m (exchange, symbol, start_time DESC);

---------------- DATA -----------------
INSERT INTO exchanges (code, name)
VALUES ('BINANCE', 'Binance Spot'),
       ('OKX', 'OKX Spot'),
       ('BYBIT', 'Bybit Spot')
ON CONFLICT (code) DO NOTHING;

---------------- MATERIALIZED VIEW -----------------
-- CREATE MATERIALIZED VIEW candles_15m
--             WITH (timescaledb.continuous) AS
-- SELECT
--     time_bucket('15 minutes', start_time) AS bucket_time,
--     symbol,
--     first(open, start_time) AS open,
--     max(high) AS high,
--     min(low) AS low,
--     last(close, start_time) AS close,
--     sum(volume) AS volume
-- FROM candles_1m
-- GROUP BY bucket_time, symbol;
--
-- SELECT add_continuous_aggregate_policy('candles_15m',
--                                        start_offset => INTERVAL '1 hour',
--                                        end_offset => INTERVAL '1 minute',
--                                        schedule_interval => INTERVAL '5 minutes');
--
-- CREATE MATERIALIZED VIEW candles_1h
--             WITH (timescaledb.continuous) AS
-- SELECT
--     time_bucket('1 hour', bucket_time) AS bucket_time,
--     symbol,
--     first(open, bucket_time) AS open,
--     max(high) AS high,
--     min(low) AS low,
--     last(close, bucket_time) AS close,
--     sum(volume) AS volume
-- FROM candles_15m
-- GROUP BY bucket_time, symbol;
--
-- SELECT add_continuous_aggregate_policy('candles_1h',
--                                        start_offset => INTERVAL '4 hours',
--                                        end_offset => INTERVAL '15 minutes',
--                                        schedule_interval => INTERVAL '15 minutes');