package user_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/goexdev/goexchange/internal/user"
)

var (
	testPool *pgxpool.Pool
	testSvc  *user.Service
)

func TestMain(m *testing.M) {
	// Connect to test DB
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
	testSvc = user.NewService(pool, log)

	os.Exit(m.Run())
}

func cleanupUser(t *testing.T, email string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email)
	require.NoError(t, err)
}

func TestRegister_Success(t *testing.T) {
	email := "register-success@test.local"
	defer cleanupUser(t, email)

	u, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, u.ID)
	assert.Equal(t, email, u.Email)
	assert.Equal(t, 0, u.KycLevel)
	assert.NotEmpty(t, u.PasswordHash)
}

func TestRegister_InvalidEmail(t *testing.T) {
	_, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    "not-an-email",
		Password: "password123",
	})
	assert.ErrorIs(t, err, user.ErrInvalidEmail)
}

func TestRegister_WeakPassword(t *testing.T) {
	_, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    "weak@test.local",
		Password: "short",
	})
	assert.ErrorIs(t, err, user.ErrWeakPassword)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	email := "duplicate@test.local"
	defer cleanupUser(t, email)

	_, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)

	_, err = testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password456",
	})
	assert.ErrorIs(t, err, user.ErrEmailTaken)
}

func TestLogin_Success(t *testing.T) {
	email := "login-success@test.local"
	defer cleanupUser(t, email)

	_, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)

	u, err := testSvc.Login(context.Background(), user.LoginInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)
	assert.Equal(t, email, u.Email)
}

func TestLogin_WrongPassword(t *testing.T) {
	email := "login-wrong@test.local"
	defer cleanupUser(t, email)

	_, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)

	_, err = testSvc.Login(context.Background(), user.LoginInput{
		Email:    email,
		Password: "WRONG",
	})
	assert.ErrorIs(t, err, user.ErrInvalidCredentials)
}

func TestLogin_NotFound(t *testing.T) {
	_, err := testSvc.Login(context.Background(), user.LoginInput{
		Email:    "nobody@test.local",
		Password: "password123",
	})
	assert.ErrorIs(t, err, user.ErrInvalidCredentials)
}

func TestGetUser_Success(t *testing.T) {
	email := "getuser@test.local"
	defer cleanupUser(t, email)

	u, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    email,
		Password: "password123",
	})
	require.NoError(t, err)

	got, err := testSvc.GetUser(context.Background(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, email, got.Email)
}

func TestGetUser_NotFound(t *testing.T) {
	_, err := testSvc.GetUser(context.Background(), uuid.New())
	assert.ErrorIs(t, err, user.ErrUserNotFound)
}

func TestRegister_DefaultIsL0(t *testing.T) {
	u, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    "default-l0-" + uuid.New().String() + "@test.local",
		Password: "password123",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, u.KycLevel, "new registered user should be L0 (default, no KYC)")
	assert.Equal(t, "NONE", u.KycStatus)
}

func TestSubmitKYC_L0ToL1(t *testing.T) {
	u, err := testSvc.Register(context.Background(), user.RegisterInput{
		Email:    "kyc-l1-" + uuid.New().String() + "@test.local",
		Password: "password123",
	})
	require.NoError(t, err)
	require.Equal(t, 0, u.KycLevel)
	
	// Submit KYC for L1
	sub, err := testSvc.SubmitKYC(context.Background(), u.ID, user.SubmitKYCInput{
		TargetLevel: 1,
		FullName:    "Test User",
		IdNumber:    "ABC123456",
		Country:     "US",
	})
	require.NoError(t, err)
	require.Equal(t, 1, sub.TargetLevel)
	require.Equal(t, "PENDING", sub.Status)
	
	// Verify user status updated
	u, err = testSvc.GetUser(context.Background(), u.ID)
	require.NoError(t, err)
	require.Equal(t, "PENDING", u.KycStatus)
	assert.NotNil(t, u.KycSubmittedAt)
	
	// Verify limit is L0 (still 1000 until approved)
	limit := user.WithdrawLimitByKYC[u.KycLevel]
	assert.Equal(t, "1000", limit.String())
	
	// Admin approves
	require.NoError(t, testSvc.ApproveKYC(context.Background(), sub.ID, "all good"))
	u, err = testSvc.GetUser(context.Background(), u.ID)
	require.NoError(t, err)
	require.Equal(t, 1, u.KycLevel)
	require.Equal(t, "APPROVED", u.KycStatus)
	assert.NotNil(t, u.KycApprovedAt)
	
	// Now limit is 10000
	limit = user.WithdrawLimitByKYC[u.KycLevel]
	assert.Equal(t, "10000", limit.String())
}
