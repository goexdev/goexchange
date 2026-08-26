package chainwallet

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/goexdev/goexchange/internal/signerclient"
)

// RemoteSigner is a Signer implementation that calls the remote signer service.
type RemoteSigner struct {
	client *signerclient.Client
}

// NewRemoteSigner creates a remote signer using the signer client.
func NewRemoteSigner(client *signerclient.Client) *RemoteSigner {
	return &RemoteSigner{client: client}
}

// Sign signs a transaction via the signer service.
func (s *RemoteSigner) Sign(ctx context.Context, chain string, txData []byte, withdrawalID, userID string) (*SignedTx, error) {
	// Wrap txData as json.RawMessage
	req := signerclient.SignRequest{
		Chain:       chain,
		TxData:      json.RawMessage(txData),
		WithdrawalID: withdrawalID,
		UserID:      userID,
	}

	resp, err := s.client.Sign(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("signer.Sign: %w", err)
	}

	return &SignedTx{
		SignedHex: resp.SignedTx,
		TxHash:    resp.TxHash,
		PubKey:    resp.PubKey,
	}, nil
}

// LocalSigner is a Signer implementation that does NOT sign.
//
// This is for development/testing where we don't have a signer service.
// It just returns the unsigned tx as the "signed" tx (which won't be valid on chain).
type LocalSigner struct{}

// NewLocalSigner creates a placeholder local signer (DO NOT USE IN PRODUCTION).
func NewLocalSigner() *LocalSigner { return &LocalSigner{} }

// Sign returns the input as "signed" without signing.
func (s *LocalSigner) Sign(_ context.Context, _ string, txData []byte, _, _ string) (*SignedTx, error) {
	return &SignedTx{
		SignedHex: string(txData), // INVALID - just for testing
		TxHash:    "INVALID_LOCAL_SIGNER",
		PubKey:    "LOCAL_SIGNER",
	}, nil
}
