# ============================================================================
# 门店本地打印代理 - 构建脚本(独立 Go module,纯标准库、零第三方依赖)
#
# 产物目录:
#   bin/     编译产物(五平台二进制,文件名带平台后缀,不入库)
#   dist/    打包制品(按操作系统分目录: darwin/ windows/ linux/,各含产物 +
#            对应平台运维脚本 + 门店安装说明,压缩包带版本号与 sha256,
#            可直接发给门店)
#
# 常用命令:
#   make build            本机编译(输出 bin/print-agent[.exe])
#   make build-all        五平台交叉编译(发版用)
#   make package          组装各平台部署包到 dist/<os>/ 并生成压缩包
#   make build-linux      交叉编译 Linux amd64 + arm64
#   make build-windows    交叉编译 Windows amd64
#   make build-darwin     交叉编译 macOS amd64 + arm64
#   make env              从模板生成本地 ./agent.env(仅首次,含令牌不入库)
#   make run              本地运行(go run,读取 ./agent.env;参数透传 ARGS)
#   make test             运行测试
#   make test-race        竞态检测
#   make vet              go vet 静态检查
#   make fmt              格式化代码
#   make clean            清理 bin/ 与 dist/
# ============================================================================

BINARY  := print-agent
OUT_DIR ?= bin
DIST_DIR := dist

# 版本信息(可通过 make VERSION=x.y.z 覆盖,与 backend/Makefile 默认一致)
VERSION   ?= 1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.buildTime=$(BUILD_TIME) \
	-X main.gitCommit=$(GIT_COMMIT)

GO_LOCAL_OS := $(shell go env GOOS)

ifeq ($(GO_LOCAL_OS),windows)
  LOCAL_BIN := $(OUT_DIR)/$(BINARY).exe
else
  LOCAL_BIN := $(OUT_DIR)/$(BINARY)
endif

.PHONY: all build build-all package \
        build-linux build-linux-amd64 build-linux-arm64 \
        build-windows build-windows-amd64 \
        build-darwin build-darwin-amd64 build-darwin-arm64 \
        env run test test-race vet fmt clean help

## all: 本机编译 + 五平台交叉编译 + 部署包
all: build build-all package

## build: 本机编译
build:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(LOCAL_BIN) .
	@echo "构建完成: $(LOCAL_BIN)"

## env: 从模板生成本地 ./agent.env(仅首次生成;随后请手工填入地址与令牌)
env:
	@if [ ! -f agent.env ]; then \
		cp agent.env.example agent.env; \
		echo "已从 agent.env.example 生成 agent.env"; \
	else \
		echo "agent.env 已存在,跳过生成"; \
	fi

## run: 本地运行(go run,读取 ./agent.env;可用 ARGS 追加参数,如 make run ARGS="--once")
run: env
	@if grep -qE "dining\.example\.com|换成你的代理令牌" agent.env; then \
		echo "agent.env 仍是示例占位值:"; \
		if [ -z "$(ARGS)" ]; then \
			echo "  已停止,三种方式任选其一:"; \
			echo "  1. 编辑 ./agent.env 填入真实 PRINT_AGENT_SERVER / PRINT_AGENT_TOKEN 后重跑 make run"; \
			echo "  2. 本地调试后端: make run ARGS=\"--server http://localhost:8080 --token <令牌> --once\""; \
			echo "  3. 门店真实安装: 编译产物执行 <print-agent> --install(交互式引导,自动注册自启动)"; \
			exit 1; \
		else \
			echo "  检测到 ARGS 显式传参,继续启动(命令行参数会覆盖 agent.env 占位值)"; \
		fi; \
	fi
	@mkdir -p $(OUT_DIR)
	go run . --env ./agent.env --state ./bin/print-agent.state $(ARGS)

## build-all: 五平台交叉编译(Linux amd64/arm64 + Windows amd64 + macOS amd64/arm64)
build-all: build-linux-amd64 build-linux-arm64 build-windows-amd64 build-darwin-amd64 build-darwin-arm64

## package: 组装各平台部署包 dist/<os>/(产物 + 对应运维脚本 + 门店安装说明),并生成带版本号的压缩包与校验和
package: build-all
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)/darwin $(DIST_DIR)/windows $(DIST_DIR)/linux
	@cp $(OUT_DIR)/$(BINARY)-darwin-amd64 $(OUT_DIR)/$(BINARY)-darwin-arm64 $(DIST_DIR)/darwin/
	@cp $(OUT_DIR)/$(BINARY)-windows-amd64.exe $(DIST_DIR)/windows/
	@cp $(OUT_DIR)/$(BINARY)-linux-amd64 $(OUT_DIR)/$(BINARY)-linux-arm64 $(DIST_DIR)/linux/
	@cp op/mac/* $(DIST_DIR)/darwin/
	@cp op/win/* $(DIST_DIR)/windows/
	@cp op/linux/* $(DIST_DIR)/linux/
	@cp agent.env.example 门店安装说明.md $(DIST_DIR)/darwin/
	@cp agent.env.example 门店安装说明.md $(DIST_DIR)/windows/
	@cp agent.env.example 门店安装说明.md $(DIST_DIR)/linux/
	@cd $(DIST_DIR)/darwin && tar -czf ../$(BINARY)-v$(VERSION)-darwin.tar.gz .
	@cd $(DIST_DIR)/linux && tar -czf ../$(BINARY)-v$(VERSION)-linux.tar.gz .
	@cd $(DIST_DIR)/windows && if python3 -m zipfile -c ../$(BINARY)-v$(VERSION)-windows.zip . 2>/dev/null; then \
		true; \
	elif command -v zip >/dev/null 2>&1; then \
		zip -rq ../$(BINARY)-v$(VERSION)-windows.zip .; \
	else \
		echo "  无 zip/python3,Windows 包退化为 tar.gz(Windows 10+ 自带 tar 可解压)"; \
		tar -czf ../$(BINARY)-v$(VERSION)-windows.tar.gz .; \
	fi
	@cd $(DIST_DIR) && for f in $(BINARY)-v$(VERSION)-*.tar.gz $(BINARY)-v$(VERSION)-*.zip; do \
		[ -f "$$f" ] && shasum -a 256 "$$f" > "$$f.sha256"; \
	done
	@echo "部署包已组装: $(DIST_DIR)/{darwin,windows,linux}/ 目录 + 压缩包 $(BINARY)-v$(VERSION)-*.tar.gz/.zip(.sha256)"

## build-linux: 交叉编译 Linux amd64 + arm64
build-linux: build-linux-amd64 build-linux-arm64

## build-linux-amd64: 交叉编译 Linux amd64(小主机/软路由)
build-linux-amd64:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY)-linux-amd64 .
	@echo "构建完成: $(OUT_DIR)/$(BINARY)-linux-amd64"

## build-linux-arm64: 交叉编译 Linux arm64(树莓派等)
build-linux-arm64:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY)-linux-arm64 .
	@echo "构建完成: $(OUT_DIR)/$(BINARY)-linux-arm64"

## build-windows: 交叉编译 Windows amd64
build-windows: build-windows-amd64

## build-windows-amd64: 交叉编译 Windows amd64(收银电脑,双击即可运行)
build-windows-amd64:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY)-windows-amd64.exe .
	@echo "构建完成: $(OUT_DIR)/$(BINARY)-windows-amd64.exe"

## build-darwin: 交叉编译 macOS amd64 + arm64
build-darwin: build-darwin-amd64 build-darwin-arm64

## build-darwin-amd64: 交叉编译 macOS Intel
build-darwin-amd64:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY)-darwin-amd64 .
	@echo "构建完成: $(OUT_DIR)/$(BINARY)-darwin-amd64"

## build-darwin-arm64: 交叉编译 macOS Apple Silicon(M1/M2/…)
build-darwin-arm64:
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" \
		-o $(OUT_DIR)/$(BINARY)-darwin-arm64 .
	@echo "构建完成: $(OUT_DIR)/$(BINARY)-darwin-arm64"

## test: 运行测试(-count=1 禁用缓存, -cover 输出覆盖率)
test:
	go test -count=1 -cover ./...

## test-race: 运行测试(竞态检测)
test-race:
	go test -race ./...

## vet: go vet 静态检查
vet:
	go vet ./...

## fmt: 格式化代码
fmt:
	gofmt -w .

## clean: 清理 bin/ 编译产物与 dist/ 部署包
clean:
	rm -rf $(OUT_DIR) $(DIST_DIR)

## help: 列出所有构建目标
help:
	@grep -E '^##' $(MAKEFILE_LIST) | sed 's/^## //'
