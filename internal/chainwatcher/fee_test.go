package chainwatcher

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestFeeFormulaMath is a unit test of the fee formula
//
// Formula: max(flat, amount * percent), then max(fee, min)
func TestFeeFormulaMath(t *testing.T) {
	tests := []struct {
		name        string
		amount      string
		feeFlat     string
		feePercent  string
		feeMin      string
		expectedFee string
	}{
		{
			name:        "BTC flat wins",
			amount:      "0.1",
			feeFlat:     "0.0005",
			feePercent:  "0.001",
			feeMin:      "0.0001",
			expectedFee: "0.0005",
		},
		{
			name:        "large amount percent wins",
			amount:      "10",
			feeFlat:     "0.0005",
			feePercent:  "0.001",
			feeMin:      "0.0001",
			expectedFee: "0.01",
		},
		{
			name:        "min wins over flat",
			amount:      "0.001",
			feeFlat:     "0.0001",
			feePercent:  "0.001",
			feeMin:      "0.0005",
			expectedFee: "0.0005",
		},
		{
			name:        "USDT stable no fee",
			amount:      "100",
			feeFlat:     "1.0",
			feePercent:  "0",
			feeMin:      "1.0",
			expectedFee: "1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, _ := decimal.NewFromString(tt.amount)
			feeFlat, _ := decimal.NewFromString(tt.feeFlat)
			feePercent, _ := decimal.NewFromString(tt.feePercent)
			feeMin, _ := decimal.NewFromString(tt.feeMin)

			feeFromPercent := amount.Mul(feePercent)
			fee := decimal.Max(feeFlat, feeFromPercent)
			fee = decimal.Max(fee, feeMin)

			expected, _ := decimal.NewFromString(tt.expectedFee)
			assert.True(t, fee.Equal(expected), "expected %s got %s", expected, fee)
		})
	}
}

// TestReceiveAmount ensures receive = amount - fee
func TestReceiveAmount(t *testing.T) {
	tests := []struct {
		name    string
		amount  string
		fee     string
		receive string
	}{
		{"normal", "0.1", "0.0005", "0.0995"},
		{"equal", "1", "1", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			amount, _ := decimal.NewFromString(tt.amount)
			fee, _ := decimal.NewFromString(tt.fee)
			receive := amount.Sub(fee)
			expected, _ := decimal.NewFromString(tt.receive)
			assert.True(t, receive.Equal(expected))
		})
	}
}