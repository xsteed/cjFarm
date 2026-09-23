# ============================================================================
# 扫码点餐管理系统 - 根构建脚本(统一编排两个独立 Go module)
#
# backend/     后端服务(module: dining-system)
# print-agent/ 门店本地打印代理(module: cjfarm/print-agent)
#
# 常用命令:
#   make test        运行全部测试(backend + print-agent,-count=1 禁用缓存)
#   make test-race   全部测试(竞态检测)
#   make vet         静态检查
#   make build       构建 backend 与 print-agent
#   make clean       清理两个 module 的产物
# ============================================================================

.PHONY: test test-race vet build clean help

## test: 运行全部测试(backend + print-agent)
test:
	@echo "==> backend"
	$(MAKE) -C backend test
	@echo "==> print-agent"
	$(MAKE) -C print-agent test

## test-race: 运行全部测试(竞态检测)
test-race:
	@echo "==> backend"
	$(MAKE) -C backend test-race
	@echo "==> print-agent"
	$(MAKE) -C print-agent test-race

## vet: 两个 module 静态检查
vet:
	@echo "==> backend"
	$(MAKE) -C backend vet
	@echo "==> print-agent"
	$(MAKE) -C print-agent vet

## build: 构建 backend 与 print-agent(本机平台)
build:
	@echo "==> backend"
	$(MAKE) -C backend build
	@echo "==> print-agent"
	$(MAKE) -C print-agent build

## clean: 清理两个 module 的构建产物
clean:
	@echo "==> backend"
	$(MAKE) -C backend clean
	@echo "==> print-agent"
	$(MAKE) -C print-agent clean

## help: 列出所有构建目标
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
