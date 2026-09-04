package api

// Test-only exports. The mmbot handlers (startMMBotHandler,
// stopMMBotHandler, mmbotStatusHandler, mmbotListHandler) are
// unexported because they are only meant to be wired by
// NewRouter. Integration tests in internal/api package use
// these re-exports to drive the handlers without having to
// construct a full chi router per test.
//
// Each ForTest wrapper takes Deps and returns the same
// http.HandlerFunc that router.go registers. They do NOT add
// admin middleware -- tests are responsible for that if they
// need it.
//
// RenderBotStateForTest exposes the stateToJSON helper so
// tests can verify the exact JSON shape admin dashboards
// consume without going through a full HTTP round-trip.

import (
	"net/http"

	"github.com/goexdev/goexchange/internal/mmbot"
)

// StartMMBotHandlerForTest returns the http.HandlerFunc for
// POST /admin/mmbot/start.
func StartMMBotHandlerForTest(d Deps) http.HandlerFunc { return startMMBotHandler(d) }

// StopMMBotHandlerForTest returns the http.HandlerFunc for
// POST /admin/mmbot/stop.
func StopMMBotHandlerForTest(d Deps) http.HandlerFunc { return stopMMBotHandler(d) }

// MMBotStatusHandlerForTest returns the http.HandlerFunc for
// GET /admin/mmbot/status.
func MMBotStatusHandlerForTest(d Deps) http.HandlerFunc { return mmbotStatusHandler(d) }

// MMBotListHandlerForTest returns the http.HandlerFunc for
// GET /admin/mmbot/list.
func MMBotListHandlerForTest(d Deps) http.HandlerFunc { return mmbotListHandler(d) }

// RenderBotStateForTest invokes the private stateToJSON helper
// and writes the result to w with Content-Type application/json.
// Tests use this to assert the exact JSON shape (e.g. decimal
// fields stay as strings) without needing to spin up a chi
// router or http.Server.
func RenderBotStateForTest(w http.ResponseWriter, _ *http.Request, s mmbot.BotState) {
	writeJSON(w, http.StatusOK, stateToJSON(s))
}
