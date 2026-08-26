#!/bin/bash
# gen_certs.sh - Generate mTLS certificates for node-proxy and chainwatcher
# 
# Run this on a secure machine (not on the node or exchange)
# It generates:
#   - CA cert (self-signed)
#   - node-proxy server cert (signed by CA)
#   - chainwatcher client cert (signed by CA)

set -e

OUTDIR="${1:-./certs}"
mkdir -p "$OUTDIR"

echo "=== Generating CA ==="
openssl genrsa -out "$OUTDIR/ca.key" 4096
openssl req -x509 -new -nodes -key "$OUTDIR/ca.key" -sha256 -days 3650 \
  -subj "/C=US/ST=CA/L=SF/O=GoExchange/CN=GoExchange CA" \
  -out "$OUTDIR/ca.crt"

echo "=== Generating node-proxy server cert ==="
openssl genrsa -out "$OUTDIR/node-proxy.key" 2048
openssl req -new -key "$OUTDIR/node-proxy.key" \
  -subj "/C=US/ST=CA/L=SF/O=GoExchange/CN=node-proxy" \
  -out "$OUTDIR/node-proxy.csr"
openssl x509 -req -in "$OUTDIR/node-proxy.csr" -CA "$OUTDIR/ca.crt" -CAkey "$OUTDIR/ca.key" \
  -CAcreateserial -out "$OUTDIR/node-proxy.crt" -days 365 -sha256
rm "$OUTDIR/node-proxy.csr"

echo "=== Generating chainwatcher client cert ==="
openssl genrsa -out "$OUTDIR/chainwatcher.key" 2048
openssl req -new -key "$OUTDIR/chainwatcher.key" \
  -subj "/C=US/ST=CA/L=SF/O=GoExchange/CN=goexchange-chainwatcher" \
  -out "$OUTDIR/chainwatcher.csr"
openssl x509 -req -in "$OUTDIR/chainwatcher.csr" -CA "$OUTDIR/ca.crt" -CAkey "$OUTDIR/ca.key" \
  -CAcreateserial -out "$OUTDIR/chainwatcher.crt" -days 365 -sha256
rm "$OUTDIR/chainwatcher.csr"

echo "=== Setting permissions ==="
chmod 600 "$OUTDIR"/*.key
chmod 644 "$OUTDIR"/*.crt

echo "=== Done ==="
ls -la "$OUTDIR"
echo ""
echo "Distribute as follows:"
echo "  ca.crt -> ALL machines (trusted root)"
echo "  node-proxy.{crt,key} -> NODE SERVERS ONLY"
echo "  chainwatcher.{crt,key} -> EXCHANGE SERVER ONLY"
