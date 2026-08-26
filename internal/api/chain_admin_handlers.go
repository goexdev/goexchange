package api

import (
	"encoding/json"
	"net/http"

	"github.com/goexdev/goexchange/internal/audit"
	"github.com/goexdev/goexchange/internal/chainwatcher"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/go-chi/chi/v5"
)

// adminListChainsHandler handles GET /api/v1/admin/chains
// Returns all configured chains with their status.
func adminListChainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.ChainRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "chain registry not initialized")
			return
		}
		configs := d.ChainRegistry.Configs()

		type chainInfo struct {
			ID          string               `json:"id"`
			Enabled     bool                 `json:"enabled"`
			Active      bool                 `json:"active"`
			Family      string               `json:"family,omitempty"`
			Driver      string               `json:"driver"`
			Asset       string               `json:"asset"`
			HotWallet   string               `json:"hot_wallet"`
			CoinType    uint32               `json:"coin_type,omitempty"`
			P2PKHPrefix byte                 `json:"p2pkh_prefix,omitempty"`
			MinConf     int                  `json:"min_conf"`
			ChainID     int64                `json:"chain_id,omitempty"`
			DisplayName string               `json:"display_name,omitempty"`
			RPCURL      string               `json:"rpc_url,omitempty"`
			ExplorerURL string               `json:"explorer_url,omitempty"`
			Tokens      []config.TokenConfig `json:"tokens,omitempty"`
			HasSigner   bool                 `json:"has_signer,omitempty"`
		}
		results := make([]chainInfo, 0, len(configs))
		for id, cfg := range configs {
			_, active := d.ChainRegistry.Get(id)
			results = append(results, chainInfo{
				ID:          id,
				Enabled:     cfg.Enabled,
				Active:      active,
			Family:      cfg.Family,
				Driver:      cfg.Driver,
				Asset:       cfg.Asset,
				HotWallet:   cfg.HotWallet,
				CoinType:    cfg.CoinType,
				P2PKHPrefix: cfg.P2PKHPrefix,
				MinConf:     cfg.MinConf,
				ChainID:     cfg.ChainID,
				DisplayName: cfg.DisplayName,
				RPCURL:      cfg.RPCURL,
				ExplorerURL: cfg.ExplorerURL,
				Tokens:      cfg.Tokens,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"chains":       results,
			"active_count": len(d.ChainRegistry.List()),
			"total_count":  len(configs),
		})
	}
}

// adminEnableChainHandler handles POST /api/v1/admin/chains/{id}/enable
func adminEnableChainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := chi.URLParam(r, "id")
		adminID := userIDFromContextUUID(r.Context())
		if chainID == "" {
			writeError(w, http.StatusBadRequest, "chain id required")
			return
		}
		if err := d.ChainRegistry.SetEnabled(chainID, true); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			AdminUserID: &adminID,
			AdminEmail:  adminID.String(),
			Action:      "chain.enable",
			TargetType:  "chain",
			TargetLabel: chainID,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chain": chainID, "enabled": true})
	}
}

// adminDisableChainHandler handles POST /api/v1/admin/chains/{id}/disable
func adminDisableChainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := chi.URLParam(r, "id")
		adminID := userIDFromContextUUID(r.Context())
		if chainID == "" {
			writeError(w, http.StatusBadRequest, "chain id required")
			return
		}
		if err := d.ChainRegistry.SetEnabled(chainID, false); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			AdminUserID: &adminID,
			AdminEmail:  adminID.String(),
			Action:      "chain.disable",
			TargetType:  "chain",
			TargetLabel: chainID,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chain": chainID, "enabled": false})
	}
}

// adminReloadChainsHandler handles POST /api/v1/admin/chains/reload
func adminReloadChainsHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID := userIDFromContextUUID(r.Context())
		if d.ChainRegistry == nil {
			writeError(w, http.StatusServiceUnavailable, "chain registry not initialized")
			return
		}
		newCfg, err := config.Load(d.ConfigPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "reload config: "+err.Error())
			return
		}
		changes, err := d.ChainRegistry.SyncFromConfig(r.Context(), newCfg.ChainWatcher.Chains)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "sync: "+err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			AdminUserID: &adminID,
			AdminEmail:  adminID.String(),
			Action:      "chain.reload",
			TargetType:  "config",
			Details:     map[string]any{"changes": changes},
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"changes": changes,
		})
	}
}

// adminAddChainHandler handles POST /api/v1/admin/chains
func adminAddChainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := chi.URLParam(r, "id")
		adminID := userIDFromContextUUID(r.Context())

		var cfg config.ChainConfig
		if err := decodeJSON(r, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
			return
		}
		if chainID != "" {
			cfg.ID = chainID
		}
		if cfg.ID == "" {
			writeError(w, http.StatusBadRequest, "chain id required (in body or URL)")
			return
		}
		if err := d.ChainRegistry.AddChain(cfg); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			AdminUserID: &adminID,
			AdminEmail:  adminID.String(),
			Action:      "chain.add",
			TargetType:  "chain",
			TargetLabel: cfg.ID,
			Details:     map[string]any{"driver": cfg.Driver, "asset": cfg.Asset},
		})
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "chain": cfg.ID})
	}
}

// adminRemoveChainHandler handles DELETE /api/v1/admin/chains/{id}
func adminRemoveChainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := chi.URLParam(r, "id")
		adminID := userIDFromContextUUID(r.Context())
		if chainID == "" {
			writeError(w, http.StatusBadRequest, "chain id required")
			return
		}
		if err := d.ChainRegistry.RemoveChain(chainID); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditLogAdmin(d, r, audit.LogEntry{
			AdminUserID: &adminID,
			AdminEmail:  adminID.String(),
			Action:      "chain.remove",
			TargetType:  "chain",
			TargetLabel: chainID,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "chain": chainID, "removed": true})
	}
}

// adminTestChainHandler handles POST /api/v1/admin/chains/{id}/test
func adminTestChainHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chainID := chi.URLParam(r, "id")
		if chainID == "" {
			writeError(w, http.StatusBadRequest, "chain id required")
			return
		}
		drv, ok := d.ChainRegistry.Get(chainID)
		if !ok {
			writeError(w, http.StatusBadRequest, "chain not active")
			return
		}
		blockCount, err := drv.GetBlockCount(r.Context())
		result := map[string]any{
			"chain":  chainID,
			"driver": drv.Name(),
		}
		if err != nil {
			result["status"] = "unhealthy"
			result["error"] = err.Error()
			writeJSON(w, http.StatusServiceUnavailable, result)
			return
		}
		result["status"] = "healthy"
		result["block_count"] = blockCount
		writeJSON(w, http.StatusOK, result)
	}
}

var _ = json.Marshal
var _ chainwatcher.Driver

// adminConfigReloadStatusHandler handles GET /api/v1/admin/config/reload
func adminConfigReloadStatusHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": true,
			"watched": d.ConfigPath,
			"message": "config is auto-reloaded on file change (500ms debounce)",
		})
	}
}
