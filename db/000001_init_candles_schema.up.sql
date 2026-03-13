CREATE EXTENSION IF NOT EXISTS timescaledb;

---------------- TABLES -----------------
CREATE TABLE candles
(
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

    UNIQUE (symbol, start_time)
);

-- Chunk candles by day
SELECT create_hypertable('candles', 'start_time', chunk_time_interval => INTERVAL '1 day');

---------------- INDEXES -----------------
CREATE INDEX ix_symbol_time ON candles (symbol, start_time DESC);