# NWDAF (Network Data Analytics Function)

NWDAF是5G核心网中的网络数据分析功能，负责收集和分析网络数据，为其他网络功能提供分析服务。

## 目录结构

本项目采用标准的Go项目结构，包含以下主要目录：

### `cmd/`
- **main.go**: 整个NF的主文件，也是编译器的入口文件，主要功能是将这个NF打包成一个命令行工具

### `internal/`
包含NWDAF的主要功能实现：

- **consumer/**: 消费者模块，负责与其他NF的通信
  - `amf_consumer.go`: AMF消费者，处理与AMF的交互
  - `nf_management.go`: NF管理相关功能

- **context/**: 上下文管理
  - `context.go`: NWDAF上下文定义和初始化

- **handler/**: 请求处理器
  - `notification_handler.go`: 通知处理器
  - `uli_handler.go`: ULI（User Location Information）处理器

- **logger/**: 日志模块
  - `logger.go`: 日志配置和初始化
  - `log.go`: 日志相关功能

- **util/**: 工具函数
  - `util.go`: 通用工具函数

### `pkg/`
包含可被其他NF使用的公共包：

- **factory/**: 配置工厂
  - `nwdaf_config.go`: 配置解析和工厂模式实现

- **service/**: 服务层
  - `nwdaf_service.go`: 将internal中的各种功能打包成一个服务供其他NF使用

## 编译和运行

```bash
# 编译
go build -o cmd/nwdaf cmd/main.go

# 运行
./cmd/nwdaf
```

## 配置

配置文件位于 `config/nwdafcfg.yaml`，包含SBI接口配置等信息。

## 功能特性

- 向NRF注册NWDAF实例
- 接收AMF的UE位置信息通知
- 支持OAuth2认证
- 提供网络数据分析服务 