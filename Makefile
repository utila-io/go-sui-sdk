# Sui gRPC (sui.rpc.v2) proto vendoring & code generation.
#
# The .proto files under clientv2/internal/pb/proto are vendored from
# MystenLabs/sui-apis at the commit pinned below, and the generated Go code is
# committed, so neither buf nor network access is needed to build or use this
# module. These targets are only needed when updating to a newer sui-apis.

SUI_APIS_REF := 87a13561361b68023b83a75ee5b27e061a4ca125
PB_DIR := clientv2/internal/pb

.PHONY: proto proto-update

# Regenerate Go bindings from the vendored protos (requires buf).
proto:
	cd $(PB_DIR) && buf generate --path proto/sui

# Re-vendor protos from MystenLabs/sui-apis at SUI_APIS_REF, then regenerate.
proto-update:
	curl -sL "https://github.com/MystenLabs/sui-apis/archive/$(SUI_APIS_REF).tar.gz" | tar -xz -C /tmp
	rm -rf $(PB_DIR)/proto
	cp -r /tmp/sui-apis-$(SUI_APIS_REF)/proto $(PB_DIR)/proto
	rm -rf /tmp/sui-apis-$(SUI_APIS_REF)
	$(MAKE) proto
