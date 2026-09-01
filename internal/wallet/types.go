// types.go: wallet V1 value types shared by the service layer,
// the scanner and the wallet-api REST handlers.
//
// These structs are intentionally separate from the legacy wallet
// types (in internal/wallet/types.go if you add one) so V1 code
// can adopt them without disturbing the bootstrap/repo path that
// has been running in production since 2026-08. The V1 service
// (service.go) embeds WalletType, Address, etc., into a richer
// model that is aware of the BlockchainAdapter abstraction.

package wallet

import "github.com/google/uuid"

// WalletType discriminates between user-facing DEPOSIT addresses,
// the company HOT wallet (sweep destination + withdrawal source),
// the COLD wallet (offline, periodic refills), and OPERATIONAL
// addresses (e.g. TRX funding for sweep).
//
// The string form is what is stored in the wallet_addresses.wallet_type
// column (migration 0029), so renaming a constant is a breaking
// change requiring a DB migration.
type WalletType string

const (
	WalletDeposit     WalletType = "DEPOSIT"
	WalletHot         WalletType = "HOT"
	WalletCold        WalletType = "COLD"
	WalletOperational WalletType = "OPERATIONAL"
)

// AllWalletTypes returns every valid WalletType. Used by the admin
// tool to populate a select box.
func AllWalletTypes() []WalletType {
	return []WalletType{WalletDeposit, WalletHot, WalletCold, WalletOperational}
}

// ParseWalletType is the inverse of AllWalletTypes. Empty strings
// resolve to WalletDeposit (the default for backwards compat) so
// legacy callers do not have to set the field explicitly.
func ParseWalletType(s string) WalletType {
	switch WalletType(s) {
	case WalletHot:
		return WalletHot
	case WalletCold:
		return WalletCold
	case WalletOperational:
		return WalletOperational
	default:
		return WalletDeposit
	}
}

// Address is the V1 view of a wallet_addresses row joined with the
// chain metadata. The Encoded form is what users see in the UI;
// Hex is what the scanner and reconciler index on.
type Address struct {
	ID        uuid.UUID  `json:"id"`
	UserID    *uuid.UUID `json:"user_id,omitempty"` // nil for company-owned HOT/COLD
	Chain     string     `json:"chain"`             // "TRON" for V1
	Asset     string     `json:"asset"`             // "USDT"
	Type      WalletType `json:"type"`
	Encoded   string     `json:"address"`           // TRON Base58Check / EVM 0x... / BTC bech32
	Hex       string     `json:"address_hex"`       // hex form for indexing
	Status    string     `json:"status"`            // "ACTIVE" | "DISABLED"
	CreatedAt string     `json:"created_at"`
}

// AllocateAddressRequest is the input to AllocateDepositAddress.
// Company wallets (HOT/COLD) ignore UserID; the V1 REST API always
// passes a user_id for DEPOSIT requests.
type AllocateAddressRequest struct {
	UserID uuid.UUID
	Chain  string
	Asset  string
}

// AllocateAddressResponse is what AllocateDepositAddress returns to
// the REST handler. The freshly-minted Address plus a flag indicating
// whether a new row was created or an existing one was reused.
type AllocateAddressResponse struct {
	Address   Address
	Reused    bool // true if we found an existing ACTIVE row for (user, chain, asset)
	NewIndex  uint32 // BIP-44 index when a new row was created
}