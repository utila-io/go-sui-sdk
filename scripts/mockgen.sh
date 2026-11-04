#!/bin/sh

cd "$(dirname "$0")/.."

# Add more source files here
sources="
suiclient/client.go
client/internal/pb/sui/rpc/v2/ledger_service_grpc.pb.go
client/internal/pb/sui/rpc/v2/state_service_grpc.pb.go
client/internal/pb/sui/rpc/v2/transaction_execution_service_grpc.pb.go
"

IFS="
"
for line in $sources; do
    file=$(echo $line | awk '{print $1}')
    case "$file" in
    "client/internal/"*)
        file=${file#"client/internal/"}
        prefix="client/internal/"
        ;;
    *)
        prefix=""
        ;;
    esac
    go tool mockgen -source="$prefix$file" -destination="${prefix}genmock/$file"
done
