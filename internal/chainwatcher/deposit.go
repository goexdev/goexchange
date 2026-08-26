package chainwatcher

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Deposit represents a deposit record.
type Deposit struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	Asset         string          `json:"asset"`
	Amount        decimal.Decimal `json:"amount"`
	TxHash        string          `json:"tx_hash"`
	Address       string          `json:"address"`
	ToAddress     string          `json:"to_address"` // alias for backward compat
	FromAddress   string          `json:"from_address"`
	Chain         string          `json:"chain"`
	Confirmations int             `json:"confirmations"`
	Status        string          `json:"status"`
	CreatedAt     time.Time       `json:"created_at"`
	CreditedAt    *time.Time      `json:"credited_at"`
}
