// Package client is the public-repository SDK for talking to the
// closed-source SignerService daemon. It is intentionally minimal:
// SignTransaction and DeriveAddress are the two RPCs the wallet
// service actually uses in V1, plus Health for the readiness
// probe on startup.
//
// The pb.go + grpc.pb.go files in this directory are vendored
// copies of pkg/grpcpkg/signerv1/* from the private goexchange-core
// repository. They are generated from the same .proto file that
// lives in core, but we keep a vendored copy here so the public
// repo can be built + tested without access to the private repo.
// Keep them in sync by running scripts/gen_proto.sh in core and
// copying the output across.
package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	signerv1 "github.com/goexdev/goexchange/internal/signer/client/signerv1"
)

// Address is the dialing target for the SignerService daemon.
// Defaults to the public-network address (host-mapped port 50061)
// for dev; in production the wallet-api container reaches the
// signer container by service name on the internal network.
const DefaultAddress = "127.0.0.1:50061"

// Client is the public-side handle for the SignerService daemon. It
// owns one gRPC connection and exposes the RPCs we use.
type Client struct {
	conn   *grpc.ClientConn
	signer signerv1.SignerServiceClient
}

// NewClient dials the signer daemon and returns a Client. Pass a
// non-default Address if the daemon is reachable on a different
// host/port (e.g. "signer:50061" when running inside the public
// docker network).
func NewClient(ctx context.Context, address string) (*Client, error) {
	if address == "" {
		address = DefaultAddress
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("dial signer %s: %w", address, err)
	}
	return &Client{
		conn:   conn,
		signer: signerv1.NewSignerServiceClient(conn),
	}, nil
}

// Close releases the underlying gRPC connection. Safe to call
// multiple times.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Sign asks the daemon to sign rawTx under keyID for the given
// chain + network. Returns the signed bytes and the network-assigned
// tx hash. On error the daemon populates resp.Error with a
// short human-readable reason (the gRPC status is also returned for
// programmatic dispatch).
func (c *Client) Sign(ctx context.Context, chain, keyID, network string, rawTx []byte) (signedTx []byte, txHash string, err error) {
	resp, err := c.signer.SignTransaction(ctx, &signerv1.SignRequest{
		Chain:   chain,
		KeyId:   keyID,
		RawTx:   rawTx,
		Network: network,
	})
	if err != nil {
		return nil, "", fmt.Errorf("signer.SignTransaction: %w", err)
	}
	if resp.Error != "" {
		return nil, "", fmt.Errorf("signer rejected: %s", resp.Error)
	}
	return resp.SignedTx, resp.TxHash, nil
}

// Derive asks the daemon to derive the address at the given index
// for the named chain. Returns the Base58Check / 0x... / bech32
// form plus the hex form for indexing.
func (c *Client) Derive(ctx context.Context, chain string, index uint32) (encoded, hexAddr, pubKey string, err error) {
	resp, err := c.signer.DeriveAddress(ctx, &signerv1.DeriveRequest{
		Chain: chain,
		Index: index,
	})
	if err != nil {
		return "", "", "", fmt.Errorf("signer.DeriveAddress: %w", err)
	}
	if resp.Error != "" {
		return "", "", "", fmt.Errorf("signer rejected: %s", resp.Error)
	}
	return resp.Address, resp.AddressHex, resp.PublicKey, nil
}

// Health probes the daemon. Returns ok=true when the daemon reports
// vault unsealed and mnemonic loaded.
func (c *Client) Health(ctx context.Context) (ok bool, vaultStatus string, chains []string, err error) {
	resp, err := c.signer.Health(ctx, &signerv1.HealthRequest{})
	if err != nil {
		return false, "", nil, fmt.Errorf("signer.Health: %w", err)
	}
	return resp.Ok, resp.VaultStatus, resp.Chains, nil
}