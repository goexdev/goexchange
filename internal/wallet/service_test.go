package wallet_test


import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goexdev/goexchange/internal/user"
	"github.com/goexdev/goexchange/internal/wallet"
)

var (
	testPool *pgxpool.Pool
	testSvc  *wallet.Service
	testUser *user.Service
)

func TestMain(m *testing.M) {
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

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	testSvc = wallet.NewService(pool, log)
	testUser = user.NewService(pool, log)

	os.Exit(m.Run())
}

// createTestUser registers a test user and returns the ID.
func createTestUser(t *testing.T) uuid.UUID {
	t.Helper()
	email := "wallet-test-" + uuid.New().String() + "@test.local"
	u, err := testUser.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)
	return u.ID
}

func cleanupTestUser(t *testing.T, id uuid.UUID) {
	t.Helper()
	_, _ = testPool.Exec(context.Background(), `DELETE FROM balances WHERE user_id = $1`, id)
	_, _ = testPool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
}

func TestBootstrapNewUser(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	// Default: faucet disabled, new user has 0 balance
	err := testSvc.BootstrapNewUser(context.Background(), id)
	require.NoError(t, err)

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "0", bal.Available.String(), "default should give 0 USDT")
	assert.True(t, bal.Frozen.IsZero())
}

func TestBootstrapNewUser_WithFaucet(t *testing.T) {
	os.Setenv("GOEXCHANGE_ENABLE_FAUCET", "true")
	defer os.Unsetenv("GOEXCHANGE_ENABLE_FAUCET")

	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	err := testSvc.BootstrapNewUser(context.Background(), id)
	require.NoError(t, err)

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "10000", bal.Available.String(), "with faucet should give 10000 USDT")
}

func TestGetAll(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	balances, err := testSvc.GetAll(context.Background(), id)
	require.NoError(t, err)

	// Should have 4 assets (BTC, ETH, BNB, USDT)
	assert.GreaterOrEqual(t, len(balances), 4)

	// Find USDT
	var usdt *wallet.Balance
	for i, b := range balances {
		if b.Asset == "USDT" {
			usdt = &balances[i]
			break
		}
	}
	require.NotNil(t, usdt)
	assert.Equal(t, "10000", usdt.Available.String())
}

func TestCredit(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	amt := decimal.NewFromInt(500)
	err := testSvc.Credit(context.Background(), id, "USDT", amt)
	require.NoError(t, err)

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "500", bal.Available.String())
}

func TestCredit_Negative(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	err := testSvc.Credit(context.Background(), id, "USDT", decimal.NewFromInt(-100))
	assert.ErrorIs(t, err, wallet.ErrNegativeAmount)
}

func TestFreeze(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	amt := decimal.NewFromInt(3000)
	err := testSvc.Freeze(context.Background(), id, "USDT", amt)
	require.NoError(t, err)

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "7000", bal.Available.String())
	assert.Equal(t, "3000", bal.Frozen.String())
}

func TestFreeze_Insufficient(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	err := testSvc.Freeze(context.Background(), id, "USDT", decimal.NewFromInt(999999))
	assert.ErrorIs(t, err, wallet.ErrInsufficientBalance)

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	// Balance unchanged
	assert.Equal(t, "10000", bal.Available.String())
	assert.True(t, bal.Frozen.IsZero())
}

func TestUnfreeze(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	frozenAmt := decimal.NewFromInt(3000)
	require.NoError(t, testSvc.Freeze(context.Background(), id, "USDT", frozenAmt))

	unfreezeAmt := decimal.NewFromInt(1000)
	require.NoError(t, testSvc.Unfreeze(context.Background(), id, "USDT", unfreezeAmt))

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "8000", bal.Available.String())
	assert.Equal(t, "2000", bal.Frozen.String())
}

func TestDebitFrozen(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))
	require.NoError(t, testSvc.Freeze(context.Background(), id, "USDT", decimal.NewFromInt(2000)))

	require.NoError(t, testSvc.DebitFrozen(context.Background(), id, "USDT", decimal.NewFromInt(500)))

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "8000", bal.Available.String())
	assert.Equal(t, "1500", bal.Frozen.String())
}

func TestDebitAvailable(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	require.NoError(t, testSvc.DebitAvailable(context.Background(), id, "USDT", decimal.NewFromInt(2500)))

	bal, err := testSvc.GetOne(context.Background(), id, "USDT")
	require.NoError(t, err)
	assert.Equal(t, "7500", bal.Available.String())
}

func TestDebitAvailable_Insufficient(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), id))

	err := testSvc.DebitAvailable(context.Background(), id, "USDT", decimal.NewFromInt(999999))
	assert.ErrorIs(t, err, wallet.ErrInsufficientBalance)
}

func TestTransfer(t *testing.T) {
	idA := createTestUser(t)
	idB := createTestUser(t)
	defer cleanupTestUser(t, idA)
	defer cleanupTestUser(t, idB)

	// Bootstrap both
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), idA))

	amt := decimal.NewFromInt(2000)
	require.NoError(t, testSvc.Transfer(context.Background(), idA, idB, "USDT", amt))

	balA, _ := testSvc.GetOne(context.Background(), idA, "USDT")
	balB, _ := testSvc.GetOne(context.Background(), idB, "USDT")

	assert.Equal(t, "8000", balA.Available.String())
	assert.Equal(t, "2000", balB.Available.String())
}

func TestTransfer_Insufficient(t *testing.T) {
	idA := createTestUser(t)
	idB := createTestUser(t)
	defer cleanupTestUser(t, idA)
	defer cleanupTestUser(t, idB)
	enableFaucetForTest(t)
	require.NoError(t, testSvc.BootstrapNewUser(context.Background(), idA))

	err := testSvc.Transfer(context.Background(), idA, idB, "USDT", decimal.NewFromInt(999999))
	assert.ErrorIs(t, err, wallet.ErrInsufficientBalance)

	// B should not have received anything
	balB, _ := testSvc.GetOne(context.Background(), idB, "USDT")
	assert.True(t, balB.Available.IsZero())
}

func TestTransfer_SameUser(t *testing.T) {
	idA := createTestUser(t)
	defer cleanupTestUser(t, idA)

	err := testSvc.Transfer(context.Background(), idA, idA, "USDT", decimal.NewFromInt(100))
	assert.ErrorIs(t, err, wallet.ErrSameUserTransfer)
}

func TestGetOne_AssetNotSupported(t *testing.T) {
	id := createTestUser(t)
	defer cleanupTestUser(t, id)

	_, err := testSvc.GetOne(context.Background(), id, "DOGE")
	assert.ErrorIs(t, err, wallet.ErrAssetNotSupported)
}

func TestSupportedAssets(t *testing.T) {
	assets, err := testSvc.SupportedAssets(context.Background())
	require.NoError(t, err)
	// Should contain core assets (may have more now)
	assert.Contains(t, assets, "BTC")
	assert.Contains(t, assets, "ETH")
	assert.Contains(t, assets, "BNB")
	assert.Contains(t, assets, "USDT")
	assert.GreaterOrEqual(t, len(assets), 4)
}

func TestBalanceListFilter(t *testing.T) {
	balances := wallet.BalanceList{
		{Asset: "BTC", Available: decimal.NewFromInt(0), Frozen: decimal.NewFromInt(0)},
		{Asset: "USDT", Available: decimal.NewFromInt(100), Frozen: decimal.NewFromInt(0)},
	}
	filtered := balances.Filter()
	assert.Len(t, filtered, 1)
	assert.Equal(t, "USDT", filtered[0].Asset)
}

// enableFaucetForTest sets GOEXCHANGE_ENABLE_FAUCET=true so BootstrapNewUser gives 10000 USDT.
// Use with t.Cleanup or defer to reset env.
func enableFaucetForTest(t *testing.T) {
	t.Helper()
	os.Setenv("GOEXCHANGE_ENABLE_FAUCET", "true")
	t.Cleanup(func() { os.Unsetenv("GOEXCHANGE_ENABLE_FAUCET") })
}
