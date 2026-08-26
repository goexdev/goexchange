#!/bin/bash
# Regenerate the gRPC stubs in internal/matching/matchingv1 from
# proto/matching.proto.
#
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc
#
# The generated files are committed so day-to-day builds don't need
# protoc on the host.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT=internal/matching/matchingv1
mkdir -p "$OUT"
rm -f "$OUT"/*.go

protoc \
    --go_out=. \
    --go_opt=module=github.com/goexdev/goexchange \
    --go-grpc_out=. \
    --go-grpc_opt=module=github.com/goexdev/goexchange \
    --proto_path=. \
    proto/matching.proto

# protoc + paths=import dumps files under github.com/.../matchingv1/;
# move them into the canonical location.
if [ -d "github.com/goexdev/goexchange/internal/matching/matchingv1" ]; then
    mv github.com/goexdev/goexchange/internal/matching/matchingv1/*.go "$OUT/"
    rm -rf github.com/
fi

echo "gRPC stubs generated:"
ls -1 "$OUT"