CREATE EXTENSION IF NOT EXISTS timescaledb;

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
-- candles_5m (from 1m)
CREATE MATERIALIZED VIEW candles_5m WITH (timescaledb.continuous) AS
SELECT
    time_bucket('5 minutes', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_1m
GROUP BY time_bucket('5 minutes', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_5m',
                                       start_offset => INTERVAL '2 hours',
                                       end_offset => INTERVAL '1 minute',
                                       schedule_interval => INTERVAL '5 minutes');

-- candles_15m (from 1m)
CREATE MATERIALIZED VIEW candles_15m WITH (timescaledb.continuous) AS
SELECT
    time_bucket('15 minutes', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_1m
GROUP BY time_bucket('15 minutes', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_15m',
                                       start_offset => INTERVAL '6 hours',
                                       end_offset => INTERVAL '1 minute',
                                       schedule_interval => INTERVAL '15 minutes');

-- candles_1h (from 15m - Hierarchical)
CREATE MATERIALIZED VIEW candles_1h WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_15m
GROUP BY time_bucket('1 hour', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_1h',
                                       start_offset => INTERVAL '24 hours',
                                       end_offset => INTERVAL '15 minutes',
                                       schedule_interval => INTERVAL '1 hour');

-- candles_1d (from 1h - Hierarchical)
CREATE MATERIALIZED VIEW candles_1d WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_1h
GROUP BY time_bucket('1 day', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_1d',
                                       start_offset => INTERVAL '7 days',
                                       end_offset => INTERVAL '1 hour',
                                       schedule_interval => INTERVAL '1 day');

-- candles_1w (from 1d - Hierarchical)
CREATE MATERIALIZED VIEW candles_1w WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 week', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_1d
GROUP BY time_bucket('1 week', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_1w',
                                       start_offset => INTERVAL '1 month',
                                       end_offset => INTERVAL '1 day',
                                       schedule_interval => INTERVAL '1 day');

-- candles_1d (from 1h - Hierarchical)
CREATE MATERIALIZED VIEW candles_1mo WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 month', start_time) AS start_time,
    exchange,
    symbol,
    first(open, start_time) AS open,
    max(high) AS high,
    min(low) AS low,
    last(close, start_time) AS close,
    sum(volume) AS volume,
    sum(quote_volume) AS quote_volume,
    sum(taker_buy_volume) AS taker_buy_volume,
    sum(number_of_trades) AS number_of_trades,
    last(end_time, start_time) AS end_time
FROM candles_1d
GROUP BY time_bucket('1 month', start_time), exchange, symbol;

SELECT add_continuous_aggregate_policy('candles_1mo',
                                       start_offset => INTERVAL '3 months',
                                       end_offset => INTERVAL '1 day',
                                       schedule_interval => INTERVAL '1 day');
