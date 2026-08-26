package chainwatcher_test

import (
	"context"
	"strings"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goexdev/goexchange/internal/chainwatcher"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/goexdev/goexchange/internal/wallet"
)

var (
	testPool   *pgxpool.Pool
	testLog    *slog.Logger
	testWallet *wallet.Service
	testUser   *user.Service
	testSvc    *chainwatcher.Service
)

// TestMain ensures faucet is enabled so tests get 10000 USDT on bootstrap.
func TestMain(m *testing.M) {
	os.Setenv("GOEXCHANGE_ENABLE_FAUCET", "true")
	defer os.Unsetenv("GOEXCHANGE_ENABLE_FAUCET")
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://exchange:exchange@localhost:5433/exchange?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		panic(err)
	}
	testPool = pool
	defer pool.Close()

	testLog = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	testWallet = wallet.NewService(pool, testLog)
	testUser = user.NewService(pool, testLog)

	cfg := config.ChainWatcherConfig{
		MockIntervalSec:  0, // disabled for tests
		MockMaxAmountUSD: 100,
	}
	testNotifier := notifier.NewService(pool, notifier.NewConsoleProvider(testLog), "test@goexchange.local", testLog)
	testSvc = chainwatcher.New(pool, testWallet, testUser, testNotifier, testLog, cfg, "mock")

	os.Exit(m.Run())
}

// makeUser creates a test user with bootstrapped USDT.
func makeUser(t *testing.T) uuid.UUID {
	t.Helper()
	email := "cw-test-" + uuid.New().String() + "@test.local"
	u, err := testUser.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)
	require.NoError(t, testWallet.BootstrapNewUser(context.Background(), u.ID))
	return u.ID
}

func cleanupUser(t *testing.T, userID uuid.UUID) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(),
		`DELETE FROM deposits WHERE user_id = $1`, userID)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM balances WHERE user_id = $1`, userID)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
}

func cleanupDeposit(t *testing.T, depositID uuid.UUID) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(), `DELETE FROM deposits WHERE id = $1`, depositID)
}

func TestSpawnDeposit_CreditsUser(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	// Get baseline balance
	balBefore, err := testWallet.GetOne(context.Background(), uid, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "10000", balBefore.Available.String())

	// Spawn deposit
	amount := decimal.NewFromInt(500)
	deposit, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", amount)
	require.NoError(t, err)
	defer cleanupDeposit(t, deposit.ID)

	assert.NotEqual(t, uuid.Nil, deposit.ID)
	assert.Equal(t, "CREDITED", deposit.Status)
	assert.True(t, strings.HasPrefix(deposit.TxHash, "MOCK_TX_"))
	assert.Equal(t, "mock", deposit.Chain)

	// Verify wallet credited
	balAfter, err := testWallet.GetOne(context.Background(), uid, "USDT")
	require.NoError(t, err)
	expected := balBefore.Available.Add(amount)
	assert.Equal(t, expected.String(), balAfter.Available.String())

	// DepositCount incremented
	assert.Equal(t, 1, testSvc.DepositsCount())
}

func TestSpawnDeposit_BTCAsset(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	amount := decimal.NewFromFloat(0.5)
	deposit, err := testSvc.SpawnDeposit(context.Background(), uid, "BTC", amount)
	require.NoError(t, err)
	defer cleanupDeposit(t, deposit.ID)

	bal, err := testWallet.GetOne(context.Background(), uid, "BTC")
	require.NoError(t, err)
	assert.Equal(t, "0.5", bal.Available.String())
}

func TestSpawnDeposit_MultipleSucceed(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	d1, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(100))
	require.NoError(t, err)
	defer cleanupDeposit(t, d1.ID)

	// Second deposit with different random txHash should succeed
	d2, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(200))
	require.NoError(t, err)
	defer cleanupDeposit(t, d2.ID)

	assert.NotEqual(t, d1.TxHash, d2.TxHash)
}

func TestSpawnDeposit_NegativeAmount(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	_, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(-100))
	assert.ErrorIs(t, err, chainwatcher.ErrInvalidAmount)
}

func TestListDeposits(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	// Spawn 3 deposits
	for i := 0; i < 3; i++ {
		d, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(int64(100+i)))
		require.NoError(t, err)
		defer cleanupDeposit(t, d.ID)
	}

	deposits, err := testSvc.ListDeposits(context.Background(), uid, 10)
	require.NoError(t, err)
	assert.Len(t, deposits, 3)

	// Newest first
	assert.Equal(t, "CREDITED", deposits[0].Status)
}

func TestGetDeposit(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	deposit, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(250))
	require.NoError(t, err)
	defer cleanupDeposit(t, deposit.ID)

	got, err := testSvc.GetDeposit(context.Background(), deposit.ID)
	require.NoError(t, err)
	assert.Equal(t, deposit.ID, got.ID)
	assert.Equal(t, "USDT", got.Asset)
	assert.Equal(t, "250", got.Amount.String())
}

func TestGetDeposit_NotFound(t *testing.T) {
	_, err := testSvc.GetDeposit(context.Background(), uuid.New())
	assert.ErrorIs(t, err, chainwatcher.ErrDepositNotFound)
}

func TestDepositCount(t *testing.T) {
	uid := makeUser(t)
	defer cleanupUser(t, uid)

	initial := testSvc.DepositsCount()

	d, err := testSvc.SpawnDeposit(context.Background(), uid, "USDT", decimal.NewFromInt(50))
	require.NoError(t, err)
	defer cleanupDeposit(t, d.ID)

	assert.Equal(t, initial+1, testSvc.DepositsCount())
}

func TestDriverName(t *testing.T) {
	cfg := config.ChainWatcherConfig{}
	d := chainwatcher.NewMockDriver(cfg)
	assert.Equal(t, "mock", d.Name())
}

func TestWithdrawLimitCheck_L0_Table(t *testing.T) {
	// Directly test the limit check using user.WithdrawLimitByKYC map
	tests := []struct {
		level       int
		amount      int64
		shouldError bool
	}{
		{0, 500, false},   // L0: 500 OK
		{0, 1000, false},  // L0: 1000 OK
		{0, 1001, true},   // L0: over limit
		{0, 1500, true},   // L0: over limit
		{1, 10001, true},  // L1: over limit
		{1, 10000, false}, // L1: at limit
		{2, 100000, false},// L2: at limit
		{2, 100001, true}, // L2: over limit
	}
	for _, tc := range tests {
		limit := user.WithdrawLimitByKYC[tc.level]
		amount := decimal.NewFromInt(tc.amount)
		exceeds := amount.GreaterThan(limit)
		assert.Equal(t, tc.shouldError, exceeds,
			"level=%d amount=%d limit=%s", tc.level, tc.amount, limit.String())
	}
}
