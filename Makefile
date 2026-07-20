# Sui gRPC (sui.rpc.v2) proto vendoring & code generation.
#
# The .proto files come from the third_party/sui-apis git submodule (the
# gitlink is the real pin; SUI_APIS_REF documents it and drives proto-update).
# Generated Go code is committed, so neither buf, the submodule, nor network
# access is needed to build or use this module. These targets are only needed
# when regenerating or updating to a newer sui-apis.

SUI_APIS_REF := 87a13561361b68023b83a75ee5b27e061a4ca125
PB_DIR := clientv2/internal/pb
SUI_APIS_DIR := third_party/sui-apis

.PHONY: proto proto-update

# Regenerate Go bindings from the submodule protos (requires buf).
#
# The output directory also holds HAND-WRITTEN files -- <proto>_convert.go
# (extending the like-named .pb.go) and convert_*.go (standalone) -- which
# implement the gRPC -> internal-types conversions. buf only ever writes
# *.pb.go, so they survive regeneration. Never add `clean: true` to
# buf.gen.yaml, never pass `buf generate --clean`, and never wipe the output
# directory: all three would delete them.
#
# buf also never REMOVES a generated file. If upstream renames or drops a
# .proto, this target writes the new .pb.go and leaves the old one behind,
# which redeclares its types and breaks the build. Delete the orphaned
# .pb.go by hand.
proto:
	git submodule update --init $(SUI_APIS_DIR)
	cd $(PB_DIR) && buf generate ../../../$(SUI_APIS_DIR) --path ../../../$(SUI_APIS_DIR)/proto/sui

# Move the submodule to SUI_APIS_REF, then regenerate.
proto-update:
	git -C $(SUI_APIS_DIR) fetch origin
	git -C $(SUI_APIS_DIR) checkout $(SUI_APIS_REF)
	$(MAKE) proto

.PHONY: mocks

# Regenerate gomock mocks (uses the go.mod tool dependency on mockgen).
mocks:
	./scripts/mockgen.sh
