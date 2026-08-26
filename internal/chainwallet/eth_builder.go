// Package chainwallet - Ethereum transaction builder.
package chainwallet

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/shopspring/decimal"
)

// EthBuilder builds Ethereum transactions.
type EthBuilder struct {
	chainID *big.Int
	nodeRPC EthNodeReader
	hotAddr string
}

// EthNodeReader reads Ethereum blockchain data via node-proxy.
type EthNodeReader interface {
	GetNonce(ctx context.Context, address string) (uint64, error)
	GetGasPrice(ctx context.Context) (*big.Int, error)
	GetChainID() *big.Int
}

// NewEthBuilder creates a new ETH tx builder.
func NewEthBuilder(chainID *big.Int, nodeRPC EthNodeReader) *EthBuilder {
	return &EthBuilder{chainID: chainID, nodeRPC: nodeRPC}
}

// SetHotAddress sets the hot wallet address.
func (b *EthBuilder) SetHotAddress(addr string) { b.hotAddr = addr }

// ChainName returns "ethereum".
func (b *EthBuilder) ChainName() string { return "ethereum" }

// Build constructs an unsigned Ethereum transaction.
func (b *EthBuilder) Build(ctx context.Context, toAddress, amount, _ string) (*UnsignedTx, error) {
	amountEth, err := decimal.NewFromString(amount)
	if err != nil {
		return nil, fmt.Errorf("parse amount: %w", err)
	}
	if !amountEth.IsPositive() {
		return nil, fmt.Errorf("amount must be positive")
	}

	amountWei := new(big.Int)
	weiDecimal := amountEth.Mul(decimal.NewFromBigInt(big.NewInt(1e18), 0))
	amountWei.SetString(weiDecimal.String(), 10)

	sourceAddr := b.hotAddr
	if sourceAddr == "" {
		sourceAddr, err = b.getHotAddress(ctx)
		if err != nil {
			return nil, fmt.Errorf("get hot address: %w", err)
		}
	}

	nonce, err := b.nodeRPC.GetNonce(ctx, sourceAddr)
	if err != nil {
		return nil, fmt.Errorf("get nonce: %w", err)
	}

	gp, err := b.nodeRPC.GetGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get gas price: %w", err)
	}

	gl := uint64(21000)

	chainID := b.chainID
	if chainID == nil {
		chainID = b.nodeRPC.GetChainID()
	}

	toAddr := common.HexToAddress(toAddress)
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gp,
		Gas:      gl,
		To:       &toAddr,
		Value:    amountWei,
		Data:     nil,
	})

	signingHash := types.LatestSignerForChainID(chainID).Hash(tx)

	raw, err := rlp.EncodeToBytes(tx)
	if err != nil {
		return nil, fmt.Errorf("rlp encode: %w", err)
	}

	fee := new(big.Int).Mul(gp, new(big.Int).SetUint64(gl))
	return &UnsignedTx{
		Chain:  "ethereum",
		TxData: raw,
		TxHash: signingHash.Hex(),
		Fee:    fee.String(),
		Nonce:  nonce,
	}, nil
}


func (b *EthBuilder) getGasPriceWei(_ context.Context, gasPriceGwei string) (*big.Int, error) {
	if gasPriceGwei != "" {
		gpGwei, err := decimal.NewFromString(gasPriceGwei)
		if err != nil {
			return nil, fmt.Errorf("parse gas price: %w", err)
		}
		gpWei := gpGwei.Mul(decimal.NewFromInt(1e9))
		gp := new(big.Int)
		gp.SetString(gpWei.String(), 10)
		return gp, nil
	}
	return b.nodeRPC.GetGasPrice(context.Background())
}

func (b *EthBuilder) getHotAddress(_ context.Context) (string, error) {
	return "", fmt.Errorf("hot address not configured")
}
