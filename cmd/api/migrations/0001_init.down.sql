-- 0001_init.down.sql
-- M0.1: Reverse initial schema

DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS mock_state;
DROP TABLE IF EXISTS deposits;
DROP TABLE IF EXISTS trades;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS balances;
DROP TABLE IF EXISTS trading_pairs;
DROP TABLE IF EXISTS currencies;
DROP TABLE IF EXISTS chains;
DROP TABLE IF EXISTS users;