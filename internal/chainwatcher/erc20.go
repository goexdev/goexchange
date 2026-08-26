package chainwatcher

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/shopspring/decimal"
)

// ERC20 transfer function selector: transfer(address,uint256)
// keccak256("transfer(address,uint256)")[:4] = 0xa9059cbb
var erc20TransferSelector = []byte{0xa9, 0x05, 0x9c, 0xbb}

// buildERC20TransferCalldata builds the calldata for ERC20 transfer(to, amount).
// Calldata format:
//   4 bytes: function selector
//   32 bytes: padded recipient address
//   32 bytes: padded amount (in token's smallest unit, e.g. wei for 18-decimal)
func buildERC20TransferCalldata(toAddress string, amount *big.Int) ([]byte, error) {
	if !common.IsHexAddress(toAddress) {
		return nil, fmt.Errorf("invalid address: %s", toAddress)
	}
	if amount == nil || amount.Sign() <= 0 {
		return nil, fmt.Errorf("amount must be positive")
	}

	addrHex := strings.TrimPrefix(toAddress, "0x")
	_ = addrHex // keep for potential debugging
	to := common.HexToAddress(toAddress)

	// Pad address to 32 bytes (12 leading zeros + 20-byte address)
	addrBytes := common.LeftPadBytes(to.Bytes(), 32)

	// Pad amount to 32 bytes
	amountBytes := common.LeftPadBytes(amount.Bytes(), 32)

	// Concat: selector + address + amount
	calldata := make([]byte, 0, 4+32+32)
	calldata = append(calldata, erc20TransferSelector...)
	calldata = append(calldata, addrBytes...)
	calldata = append(calldata, amountBytes...)
	return calldata, nil
}

// sendERC20 sends an ERC20/BEP-20 token transfer.
//
// Steps:
//  1. Build calldata: transfer(to, amount)
//  2. Query nonce from hot wallet
//  3. Get gas price
//  4. Estimate gas (for token transfer, usually ~60k-100k)
//  5. Build LegacyTx: to=token_contract, value=0, data=calldata
//  6. Sign with EIP-155
//  7. Broadcast via eth_sendRawTransaction
func (d *EVMDriver) sendERC20(ctx context.Context, tokenContract, toAddress string, amount *big.Int) (string, error) {
	if d.privKey == nil {
		return "", fmt.Errorf("EVM driver %s has no private key", d.name)
	}
	if !common.IsHexAddress(tokenContract) {
		return "", fmt.Errorf("invalid token contract: %s", tokenContract)
	}

	// Build calldata
	calldata, err := buildERC20TransferCalldata(toAddress, amount)
	if err != nil {
		return "", fmt.Errorf("build calldata: %w", err)
	}

	// Get nonce
	fromAddr := d.signer.Address()
	nonce, err := d.getUint(ctx, "eth_getTransactionCount",
		[]interface{}{fromAddr, "pending"})
	if err != nil {
		return "", fmt.Errorf("get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := d.getUint(ctx, "eth_gasPrice", nil)
	if err != nil {
		return "", fmt.Errorf("get gas price: %w", err)
	}

	// Estimate gas (optional, fall back to 100k if fails)
	gasLimit := uint64(100000)
	gasHex, err := d.rpcCall(ctx, "eth_estimateGas", []interface{}{
		map[string]interface{}{
			"from":  fromAddr,
			"to":    tokenContract,
			"value": "0x0",
			"data":  "0x" + common.Bytes2Hex(calldata),
		},
	})
	if err == nil {
		var gasStr string
		if err := parseRPCResult(gasHex, &gasStr); err == nil {
			estimated, ok := new(big.Int).SetString(strings.TrimPrefix(gasStr, "0x"), 16)
			if ok {
				// Add 20% buffer
				gasLimit = estimated.Uint64() * 120 / 100
			}
		}
	}

	// Build EIP-155 transaction
	chainID := big.NewInt(d.chainID)
	tokenAddr := common.HexToAddress(tokenContract)

	// Build transaction - EIP-1559 if supported, else legacy
	var tx *types.Transaction
	if d.supportsEIP1559(ctx) {
		baseFee, err := d.getBaseFee(ctx)
		if err != nil {
			return "", fmt.Errorf("get base fee: %w", err)
		}
		tipCap, err := d.getMaxPriorityFee(ctx)
		if err != nil {
			return "", fmt.Errorf("get priority fee: %w", err)
		}
		maxFeePerGas := new(big.Int).Add(
			new(big.Int).Mul(baseFee, big.NewInt(2)),
			tipCap,
		)
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce.Uint64(),
			GasTipCap: tipCap,
			GasFeeCap: maxFeePerGas,
			Gas:       gasLimit,
			To:        &tokenAddr,
			Value:     big.NewInt(0),
			Data:      calldata,
		})
	} else {
		tx = types.NewTx(&types.LegacyTx{
			Nonce:    nonce.Uint64(),
			GasPrice: gasPrice,
			Gas:      gasLimit,
			To:       &tokenAddr,
			Value:    big.NewInt(0),
			Data:     calldata,
		})
	}

	// Sign
	signer := types.LatestSignerForChainID(chainID)
	signedTx, err := types.SignTx(tx, signer, d.privKey)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}

	// Marshal
	rawBytes, err := signedTx.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("marshal tx: %w", err)
	}

	// Broadcast
	rawHex := "0x" + common.Bytes2Hex(rawBytes)
	resp, err := d.rpcCall(ctx, "eth_sendRawTransaction", []interface{}{rawHex})
	if err != nil {
		return "", fmt.Errorf("send tx: %w", err)
	}
	var txHash string
	if err := parseRPCResult(resp, &txHash); err != nil {
		return "", fmt.Errorf("parse result: %w", err)
	}
	return txHash, nil
}

// parseRPCResult unmarshals a JSON-RPC result field into v.
func parseRPCResult(raw []byte, v interface{}) error {
	return json.Unmarshal(raw, v)
}

// amountToBigInt converts a decimal amount to big.Int with proper decimals.
// e.g. decimal "1.5" with 18 decimals -> big.NewInt(1500000000000000000)
func amountToBigInt(amount decimal.Decimal, decimals int) *big.Int {
	multiplier := decimal.New(1, int32(decimals))
	return amount.Mul(multiplier).BigInt()
}