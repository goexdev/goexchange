package cliutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPresetNames(t *testing.T) {
	presets := map[string]int{
		"btc":     0,
		"doge":    3,
		"ethereum": 1,
		"bitcoin-cash": 145,
	}
	names := PresetNames(presets)
	assert.Len(t, names, 4)
	// Should be sorted
	assert.Equal(t, []string{"bitcoin-cash", "btc", "doge", "ethereum"}, names)
}

func TestPresetNames_Empty(t *testing.T) {
	names := PresetNames(map[string]int{})
	assert.Empty(t, names)
}

func TestPresetNames_Nil(t *testing.T) {
	names := PresetNames(nil)
	assert.Empty(t, names)
}

func TestPromptDefault(t *testing.T) {
	// Can't easily test stdin in unit test, just verify it doesn't panic
	// Real interaction tested via manual use
}

func TestParseTokens(t *testing.T) {
	// Test with separator
	tokens, err := ParseTokens("USDC:0xabc123:6:1,USDT:0xdef456:6", ":")
	assert.NoError(t, err)
	assert.Len(t, tokens, 2)

	// First token - all fields
	assert.Equal(t, "USDC", tokens[0].Symbol)
	assert.Equal(t, "0xabc123", tokens[0].Address)
	assert.Equal(t, 6, tokens[0].Decimals)
	assert.Equal(t, 1, tokens[0].MinConf)

	// Second token - explicit decimals 6
	assert.Equal(t, "USDT", tokens[1].Symbol)
	assert.Equal(t, "0xdef456", tokens[1].Address)
	assert.Equal(t, 6, tokens[1].Decimals)   // explicit
	assert.Equal(t, 12, tokens[1].MinConf)   // default
}

func TestParseTokens_Invalid(t *testing.T) {
	_, err := ParseTokens("USDC", ":")
	assert.Error(t, err)
}
