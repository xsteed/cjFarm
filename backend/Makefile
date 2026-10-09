# ============================================================================
# 扫码点餐管理系统 - 后端构建脚本
#
# 依赖: Go 1.23+（纯 Go SQLite 驱动 modernc.org/sqlite, 无 CGO 依赖）
# 说明: 所有二进制输出到 bin/ 目录; 支持 Linux 后端交叉编译与五平台打印代理交叉编译。
#
# 常用命令:
#   make build            本机编译, 输出 bin/dining-backend(.exe) + bin/config.yaml(生产配置)
#   make config           仅生成/刷新 bin/config.yaml(优先 config.prod.yaml, 缺失回退 config.example.yaml)
#   make package          本机编译 + 生成交付目录 bin/(二进制 + config.yaml)
#   make build-linux      交叉编译 Linux amd64 + arm64
#   make build-linux-amd64  交叉编译 Linux amd64
#   make build-linux-arm64  交叉编译 Linux arm64
#   make build-agent      交叉编译门店打印代理(Linux amd64/arm64 + Windows amd64 + macOS amd64/arm64)
#   make run              本地运行(go run, 不产出二进制)
#   make dev              本地编译并运行(推荐开发用)
#   make test             运行测试
#   make vet              go vet 静态检查
#   make fmt              格式化代码
#   make tidy             整理依赖
#   make clean            清理 bin/ 产物
# ============================================================================

BINARY     := dining-backend
BIN_DIR    := bin
MAIN_PKG   := .
# 生产环境配置: 编译时复制为 bin/config.yaml, 部署时作为生成 config.yaml 的首选来源。
# 这是**本机私有文件**(已被 .gitignore 排除, 可安全填写真实密码); 仓库里公开的是
# CONFIG_EXAMPLE, 缺失时由它兜底。新克隆的仓库请先: cp config.example.yaml config.prod.yaml
PROD_CONFIG    := config.prod.yaml
CONFIG_EXAMPLE := config.example.yaml
# 门店本地打印代理(部署在门店内网,详见 docs/print-agent.md)
AGENT_BINARY := print-agent
AGENT_DIR    := ../print-agent

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
  AGENT_LOCAL_BIN := $(AGENT_DIR)/bin/$(AGENT_BINARY).exe
else
  LOCAL_BIN := $(BIN_DIR)/$(BINARY)
  AGENT_LOCAL_BIN := $(AGENT_DIR)/bin/$(AGENT_BINARY)
endif

.PHONY: all build build-linux build-linux-amd64 build-linux-arm64 \
        build-agent build-agent-linux build-agent-linux-amd64 build-agent-linux-arm64 \
        build-agent-windows-amd64 build-agent-darwin-amd64 build-agent-darwin-arm64 \
        config package dev run test test-race vet fmt tidy clean help

## all: 本机编译 + Linux 后端交叉编译 + 五平台门店打印代理
all: build build-linux build-agent

## config: 生成 bin/config.yaml(优先 config.prod.yaml, 缺失时回退 config.example.yaml)
# config.prod.yaml 是本机私有文件(不入库, 可写真实密码), 每次都据此覆盖 bin/config.yaml ——
# bin/ 是编译产物目录, 要改配置请改 config.prod.yaml。
config:
	@mkdir -p $(BIN_DIR)
	@if [ -f $(PROD_CONFIG) ]; then \
	  cp -f $(PROD_CONFIG) $(BIN_DIR)/config.yaml; \
	  echo "生产配置: $(BIN_DIR)/config.yaml (来源: $(PROD_CONFIG))"; \
	else \
	  cp -f $(CONFIG_EXAMPLE) $(BIN_DIR)/config.yaml; \
	  echo "[warn] 未找到 $(PROD_CONFIG)(本机私有文件, 不入库), 已回退 $(CONFIG_EXAMPLE)"; \
	  echo "       建议: cp $(CONFIG_EXAMPLE) $(PROD_CONFIG) 后按需填写。"; \
	fi

## build: 本机编译 + 生成 bin/config.yaml
build: config
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(LOCAL_BIN) $(MAIN_PKG)
	@echo "构建完成: $(LOCAL_BIN)"

## package: 本机编译 + 生成交付目录 bin/(二进制 + config.yaml)
# 配置这件事只需把 config.prod.yaml 复制成 bin/config.yaml —— 由 build(-> config) 完成,
# 所以这里不再额外放模板文件, bin/ 保持「二进制 + 一份配置」的干净结构。
package: build
	@echo "打包完成: $(BIN_DIR)/ (二进制 + config.yaml)"

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

# ---- 门店本地打印代理(运行在门店内网,详见 docs/print-agent.md) ----
#
# 代理是顶层独立 module(print-agent/),构建时切目录执行,产物统一输出到
# print-agent/bin(代理专属产物目录;部署包由 print-agent/Makefile 的 package 组装)。
# 代理要跑在门店那台常开机的设备上,而开发机与门店设备的系统往往不同,
# 因此这里把常见的五种目标平台都列出来,按门店实际情况挑一个拷过去即可。

AGENT_OUT := $(CURDIR)/$(AGENT_DIR)/bin

## build-agent: 交叉编译门店打印代理(Linux amd64/arm64 + Windows amd64 + macOS amd64/arm64)
build-agent: build-agent-linux-amd64 build-agent-linux-arm64 build-agent-windows-amd64 build-agent-darwin-amd64 build-agent-darwin-arm64

## build-agent-linux: 交叉编译代理 Linux amd64 + arm64
build-agent-linux: build-agent-linux-amd64 build-agent-linux-arm64

## build-agent-linux-amd64: 交叉编译代理 Linux amd64(小主机/软路由)
build-agent-linux-amd64:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(AGENT_OUT)/$(AGENT_BINARY)-linux-amd64 .
	@echo "构建完成: $(AGENT_DIR)/bin/$(AGENT_BINARY)-linux-amd64"

## build-agent-linux-arm64: 交叉编译代理 Linux arm64(树莓派等)
build-agent-linux-arm64:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(AGENT_OUT)/$(AGENT_BINARY)-linux-arm64 .
	@echo "构建完成: $(AGENT_DIR)/bin/$(AGENT_BINARY)-linux-arm64"

## build-agent-windows-amd64: 交叉编译代理 Windows amd64(收银电脑,双击即可运行)
build-agent-windows-amd64:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(AGENT_OUT)/$(AGENT_BINARY)-windows-amd64.exe .
	@echo "构建完成: $(AGENT_DIR)/bin/$(AGENT_BINARY)-windows-amd64.exe"

## build-agent-darwin-amd64: 交叉编译代理 macOS Intel
build-agent-darwin-amd64:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(AGENT_OUT)/$(AGENT_BINARY)-darwin-amd64 .
	@echo "构建完成: $(AGENT_DIR)/bin/$(AGENT_BINARY)-darwin-amd64"

## build-agent-darwin-arm64: 交叉编译代理 macOS Apple Silicon(M1/M2/…)
build-agent-darwin-arm64:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(AGENT_OUT)/$(AGENT_BINARY)-darwin-arm64 .
	@echo "构建完成: $(AGENT_DIR)/bin/$(AGENT_BINARY)-darwin-arm64"

## build-agent-local: 按当前系统编译代理(本机调试用)
build-agent-local:
	@mkdir -p $(AGENT_OUT)
	cd $(AGENT_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(CURDIR)/$(AGENT_LOCAL_BIN) .
	@echo "构建完成: $(AGENT_LOCAL_BIN)"

## dev: 本地编译并运行(先 build 出 bin/ 产物再执行, 便于验证真实二进制)
dev: build
	@echo "启动开发服务(工作目录: $(CURDIR))..."
	./$(LOCAL_BIN)

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
