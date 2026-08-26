// Package cliutil provides shared utilities for the cmd/* CLI tools.
//
// Common functionality used by add_chain, add_solana, btc_chain, etc.
package cliutil

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"
)

// Prompt asks the user for input via stdin.
// Returns the trimmed input string.
func Prompt(reader *bufio.Reader, question string) string {
	fmt.Printf("%s: ", question)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

// PromptDefault asks the user for input, returning defaultValue if empty.
func PromptDefault(reader *bufio.Reader, question, defaultValue string) string {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", question, defaultValue)
	} else {
		fmt.Printf("%s: ", question)
	}
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue
	}
	return text
}

// PromptInt asks the user for an integer, returning defaultValue if empty.
// Returns defaultValue on parse error.
func PromptInt(reader *bufio.Reader, question string, defaultValue int) int {
	text := PromptDefault(reader, question, strconv.Itoa(defaultValue))
	if text == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return defaultValue
	}
	return v
}

// ReadConfigFile reads and parses a YAML config file into the given target.
func ReadConfigFile(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	return nil
}

// WriteConfigFile serializes the given target to YAML and writes to path.
func WriteConfigFile(path string, source interface{}) error {
	out, err := yaml.Marshal(source)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, out, 0644)
}

// ConnectDB opens a PostgreSQL connection pool.
// Returns nil if dbURL is empty.
func ConnectDB(dbURL string) (*pgxpool.Pool, error) {
	if dbURL == "" {
		return nil, fmt.Errorf("database URL required")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

// InsertChain inserts or updates a chain in the chains table.
// chain_id is 0 for chains that don't have one (e.g. BTC forks).
func InsertChain(ctx context.Context, pool *pgxpool.Pool, name string, chainID int, asset string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO chains (name, chain_id, native_currency, enabled)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET
			chain_id = EXCLUDED.chain_id,
			native_currency = EXCLUDED.native_currency,
			enabled = EXCLUDED.enabled
	`, name, chainID, asset, true)
	if err != nil {
		return fmt.Errorf("insert chain: %w", err)
	}
	return nil
}

// InsertCurrency inserts or updates a currency.
func InsertCurrency(ctx context.Context, pool *pgxpool.Pool, symbol, name string, precision int) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO currencies (symbol, name, precision, is_active, min_withdraw, max_withdraw)
		VALUES ($1, $2, $3, TRUE, 0, 0)
		ON CONFLICT (symbol) DO UPDATE SET
			name = EXCLUDED.name,
			precision = EXCLUDED.precision,
			is_active = TRUE
	`, symbol, name, precision)
	if err != nil {
		return fmt.Errorf("insert currency: %w", err)
	}
	return nil
}

// PresetNames returns sorted preset names from a map (for help messages).
func PresetNames(presets interface{}) []string {
	// Use reflection to iterate map keys
	v := reflect.ValueOf(presets)
	if v.Kind() != reflect.Map {
		return nil
	}
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		if s, ok := k.Interface().(string); ok {
			keys = append(keys, s)
		}
	}
	sort.Strings(keys)
	return keys
}

// InsertCurrencies inserts multiple currencies in one call.
func InsertCurrencies(ctx context.Context, pool *pgxpool.Pool, currencies []CurrencySpec) error {
	for _, c := range currencies {
		if err := InsertCurrency(ctx, pool, c.Symbol, c.Name, c.Precision); err != nil {
			return fmt.Errorf("insert currency %s: %w", c.Symbol, err)
		}
	}
	return nil
}

// CurrencySpec describes a currency to insert.
type CurrencySpec struct {
	Symbol    string
	Name      string
	Precision int
}

// PrintSuccess prints a success message with a checkmark.
func PrintSuccess(msg string) {
	fmt.Printf("\u2713 %s\n", msg)
}

// PrintStep prints a step header.
func PrintStep(n int, msg string) {
	fmt.Printf("\n[%d] %s\n", n, msg)
}

// ParseTokens parses a comma-separated list of tokens in the format SYMBOL:ADDRESS[:DECIMALS[:MIN_CONF]].
// Returns parsed tokens or an error.
func ParseTokens(arg string, separator string) ([]ParsedToken, error) {
	var tokens []ParsedToken
	for _, entry := range strings.Split(arg, ",") {
		parts := strings.Split(strings.TrimSpace(entry), separator)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid token format: %s (expected SYMBOL:ADDRESS[:DECIMALS[:MIN_CONF]])", entry)
		}
		t := ParsedToken{
			Symbol:   strings.ToUpper(parts[0]),
			Address:  parts[1],
			Decimals: 18,
			MinConf:  12,
		}
		if len(parts) >= 3 {
			d, err := strconv.Atoi(parts[2])
			if err != nil {
				return nil, fmt.Errorf("invalid decimals for %s: %s", t.Symbol, parts[2])
			}
			t.Decimals = d
		}
		if len(parts) >= 4 {
			mc, err := strconv.Atoi(parts[3])
			if err != nil {
				return nil, fmt.Errorf("invalid min_conf for %s: %s", t.Symbol, parts[3])
			}
			t.MinConf = mc
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// ParsedToken is a token parsed from command-line arguments.
type ParsedToken struct {
	Symbol   string
	Address  string
	Decimals int
	MinConf  int
}
