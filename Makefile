.PHONY: build build-cli test lint proto clean package-ext install-ext help

# Default Go binary output
ENGINE_BIN := apix-engine
CLI_BIN := apix-cli

# Go build
build:
	go build -o $(ENGINE_BIN) ./cmd/apix-engine/

# CLI build
build-cli:
	go build -o $(CLI_BIN) ./cmd/apix-cli/

# Go tests
test:
	go test ./... -count=1

# Go tests with coverage
test-coverage:
	go test ./internal/... -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out

# Run a single test (usage: make test-one TEST=TestSaveAndGetRequest PKG=./internal/storage/)
test-one:
	go test $(PKG) -run $(TEST) -v -count=1

# Go vet
lint:
	go vet ./...

# Regenerate proto (VS Code extension uses a symlink — no copy needed)
proto:
	protoc --go_out=pkg/api/generated --go-grpc_out=pkg/api/generated \
		--go_opt=paths=source_relative --go-grpc_opt=paths=source_relative \
		pkg/api/proto/apix.proto

# Build VS Code extension
ext-build: build
	mkdir -p apix-vscode/bin
	cp $(ENGINE_BIN) apix-vscode/bin/apix-engine
	cd apix-vscode && npm install && npm run compile

# Package VS Code extension as .vsix
ext-package:
	cd apix-vscode && npx vsce package

# Install extension locally
ext-install:
	cd apix-vscode && code --install-extension *.vsix

# Clean build artifacts
clean:
	rm -f $(ENGINE_BIN) $(CLI_BIN) coverage.out
	rm -rf apix-vscode/out apix-vscode/*.vsix

# Cross-compile engine for all platforms
build-all:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 go build -o dist/apix-engine-darwin-arm64 ./cmd/apix-engine/
	GOOS=darwin GOARCH=arm64 go build -o dist/apix-cli-darwin-arm64 ./cmd/apix-cli/
	GOOS=darwin GOARCH=amd64 go build -o dist/apix-engine-darwin-amd64 ./cmd/apix-engine/
	GOOS=darwin GOARCH=amd64 go build -o dist/apix-cli-darwin-amd64 ./cmd/apix-cli/
	GOOS=linux GOARCH=amd64 go build -o dist/apix-engine-linux-amd64 ./cmd/apix-engine/
	GOOS=linux GOARCH=amd64 go build -o dist/apix-cli-linux-amd64 ./cmd/apix-cli/
	GOOS=linux GOARCH=arm64 go build -o dist/apix-engine-linux-arm64 ./cmd/apix-engine/
	GOOS=linux GOARCH=arm64 go build -o dist/apix-cli-linux-arm64 ./cmd/apix-cli/
	GOOS=windows GOARCH=amd64 go build -o dist/apix-engine-windows-amd64.exe ./cmd/apix-engine/
	GOOS=windows GOARCH=amd64 go build -o dist/apix-cli-windows-amd64.exe ./cmd/apix-cli/

# Dev: build engine + extension together
dev: build ext-build

smoke:
	sh tests/release/smoke.sh

help:
	@echo "Available targets:"
	@echo "  build          - Build engine binary"
	@echo "  build-cli      - Build CLI binary"
	@echo "  test           - Run Go tests"
	@echo "  test-coverage  - Run Go tests with coverage report"
	@echo "  test-one       - Run single test (TEST=name PKG=path)"
	@echo "  lint           - Run go vet"
	@echo "  proto          - Regenerate protobuf code"
	@echo "  ext-build      - Build VS Code extension"
	@echo "  ext-package    - Package extension as .vsix"
	@echo "  ext-install    - Install extension locally"
	@echo "  build-all      - Cross-compile for all platforms"
	@echo "  dev            - Build everything"
	@echo "  clean          - Remove build artifacts"
