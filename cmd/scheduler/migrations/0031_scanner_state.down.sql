-- 0031 DOWN: drop scanner_state.
--
-- runs of the DOWN migration (truncate-and-rebuild for a clean
-- test, or rollback) wipe the cursor along with everything else.

DROP TABLE IF EXISTS scanner_state;