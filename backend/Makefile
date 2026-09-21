# ============================================================================
# 扫码点餐管理系统 - 后端构建脚本
#
# 依赖: Go 1.23+（纯 Go SQLite 驱动 modernc.org/sqlite, 无 CGO 依赖）
# 说明: 所有二进制输出到 bin/ 目录; 支持 Linux 交叉编译 (amd64/arm64)。
#
# 常用命令:
#   make build            本机编译, 输出 bin/dining-server(.exe)
#   make build-linux      交叉编译 Linux amd64 + arm64
#   make build-linux-amd64  交叉编译 Linux amd64
#   make build-linux-arm64  交叉编译 Linux arm64
#   make run              本地运行
#   make test             运行测试
#   make vet              go vet 静态检查
#   make fmt              格式化代码
#   make tidy             整理依赖
#   make clean            清理 bin/ 产物
# ============================================================================

BINARY     := dining-server
BIN_DIR    := bin
MAIN_PKG   := .

# 版本信息(可通过 make VERSION=x.y.z 覆盖)
VERSION    ?= 1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.buildTime=$(BUILD_TIME) \
	-X main.gitCommit=$(GIT_COMMIT)

# 本机 GOOS/GOARCH
GOOS_LOCAL   := $(shell go env GOOS)
GOARCH_LOCAL := $(shell go env GOARCH)

ifeq ($(GOOS_LOCAL),windows)
  LOCAL_BIN := $(BIN_DIR)/$(BINARY).exe
else
  LOCAL_BIN := $(BIN_DIR)/$(BINARY)
endif

.PHONY: all build build-linux build-linux-amd64 build-linux-arm64 \
        run test test-race vet fmt tidy clean help

## all: 本机编译 + Linux 交叉编译
all: build build-linux

## build: 本机编译
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(LOCAL_BIN) $(MAIN_PKG)
	@echo "构建完成: $(LOCAL_BIN)"

## build-linux: 交叉编译 Linux amd64 + arm64
build-linux: build-linux-amd64 build-linux-arm64

## build-linux-amd64: 交叉编译 Linux amd64 (x86_64)
build-linux-amd64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/$(BINARY)-linux-amd64 $(MAIN_PKG)
	@echo "构建完成: $(BIN_DIR)/$(BINARY)-linux-amd64"

## build-linux-arm64: 交叉编译 Linux arm64 (aarch64)
build-linux-arm64:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(BIN_DIR)/$(BINARY)-linux-arm64 $(MAIN_PKG)
	@echo "构建完成: $(BIN_DIR)/$(BINARY)-linux-arm64"

## run: 本地运行(自动建库/种子数据)
run:
	go run $(MAIN_PKG)

## test: 运行全部测试(-count=1 禁用缓存, -cover 输出覆盖率)
test:
	go test -count=1 -cover ./...

## test-race: 运行测试并开启竞态检测(需 CGO 工具链, 如本机装有 gcc)
test-race:
	go test -race -count=1 ./...

## vet: 静态检查
vet:
	go vet ./...

## fmt: 格式化代码(递归 internal/ 子包)
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './bin/*')

## tidy: 整理依赖
tidy:
	go mod tidy

## clean: 清理构建产物
clean:
	rm -rf $(BIN_DIR)
	@echo "已清理 $(BIN_DIR)"

## help: 显示帮助
help:
	@echo "可用目标:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
