-- 0030: Disable BTC and ETH trading pairs.
--
-- Small exchange scope (BOSS 2026-09-01): the only pairs we want
-- users to trade are BNB/USDT and SOL/USDT. BTC and ETH stay in the
-- database so the Web UI can still display them as coming-soon, but
-- their enabled flag is flipped off so the matching engine, ticker
-- feed, order-book endpoint and admin/place-order gate all reject
-- them with 404 / 400.
--
-- This replaces the ad-hoc UPDATE we previously issued by hand on the
-- live database; deploy-fresh drops + recreates the DB on every run,
-- so a single migration guarantees the policy survives fresh deploys.
--
-- Pre-existing test rows are touched in place rather than dropped;
-- the matching engine, marketdata service and trading_handlers
-- check enabled on read so flipping the flag is sufficient.

UPDATE trading_pairs SET enabled = FALSE
WHERE base IN ('BTC', 'ETH');