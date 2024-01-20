protos:
	protoc -I=./ --go_opt=paths=source_relative --go_out=./ pb/transactions.proto
	protoc -I=./ --go_opt=paths=source_relative --go_out=./ pb/blocks.proto
	protoc -I=./ --go_opt=paths=source_relative --go_out=./ --go-grpc_out=./ --go-grpc_opt=paths=source_relative pb/ilxrpc.proto

install:
	go mod tidy
	$(eval RUST_LIB_PATH=$(shell go list -m -f "{{.Dir}}" github.com/project-illium/ilxd))
	chmod -R u+w $(RUST_LIB_PATH)
	cd $(RUST_LIB_PATH)/crypto/rust && cargo build --release
	cd $(RUST_LIB_PATH)/zk/rust && cargo build --release
	chmod -R u-w $(RUST_LIB_PATH)
	go install
