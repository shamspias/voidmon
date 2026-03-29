APP_NAME := voidmon
BIN_NAME := void
VERSION := 1.0.0
BUILD_DIR := build
GOFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build clean install uninstall run

all: build

build:
	@echo "  ░▒▓ Building $(APP_NAME) v$(VERSION) ▓▒░"
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -o $(BUILD_DIR)/$(BIN_NAME) .
	@echo "  ✓ Binary: $(BUILD_DIR)/$(BIN_NAME)"
	@echo "  ✓ Run with: ./$(BUILD_DIR)/$(BIN_NAME)"

run: build
	./$(BUILD_DIR)/$(BIN_NAME)

install: build
	@echo "  Installing $(BIN_NAME) to /usr/local/bin/"
	@mkdir -p /usr/local/bin
	@cp $(BUILD_DIR)/$(BIN_NAME) /usr/local/bin/$(BIN_NAME)
	@echo "  ✓ Installed. Run with: $(BIN_NAME)"

uninstall:
	@echo "  Removing $(BIN_NAME) from /usr/local/bin/"
	@rm -f /usr/local/bin/$(BIN_NAME)
	@echo "  ✓ Uninstalled."

clean:
	rm -rf $(BUILD_DIR)
	@echo "  ✓ Cleaned."

# ─── Cross-compilation ───────────────────────

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BIN_NAME)-linux-amd64 .

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BIN_NAME)-linux-arm64 .

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BIN_NAME)-darwin-arm64 .

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(GOFLAGS) -o $(BUILD_DIR)/$(BIN_NAME)-darwin-amd64 .

build-all: build-linux-amd64 build-linux-arm64 build-darwin-arm64 build-darwin-amd64
	@echo "  ✓ All platforms built in $(BUILD_DIR)/"
	@ls -lh $(BUILD_DIR)/

# ─── Release packaging ──────────────────────

release: build-all
	@mkdir -p $(BUILD_DIR)/release
	@cd $(BUILD_DIR) && tar -czf release/$(BIN_NAME)-$(VERSION)-linux-amd64.tar.gz $(BIN_NAME)-linux-amd64
	@cd $(BUILD_DIR) && tar -czf release/$(BIN_NAME)-$(VERSION)-linux-arm64.tar.gz $(BIN_NAME)-linux-arm64
	@cd $(BUILD_DIR) && tar -czf release/$(BIN_NAME)-$(VERSION)-darwin-arm64.tar.gz $(BIN_NAME)-darwin-arm64
	@cd $(BUILD_DIR) && tar -czf release/$(BIN_NAME)-$(VERSION)-darwin-amd64.tar.gz $(BIN_NAME)-darwin-amd64
	@echo "  ✓ Release tarballs in $(BUILD_DIR)/release/"