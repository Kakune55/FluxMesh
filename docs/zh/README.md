# FluxMesh 文档

本文档集描述当前代码实现。
代码与文档冲突时，以代码为准。

## 阅读顺序

1. 阅读[快速开始](getting-started.md)。
2. 阅读[项目架构](architecture/overview.md)。
3. 按需阅读各模块设计。
4. 联调时查看[API 与配置参考](reference/api-reference.md)。

## 文档结构

| 文档 | 内容 |
| --- | --- |
| [快速开始](getting-started.md) | 构建、启动和基础验证 |
| [项目架构](architecture/overview.md) | 系统边界、组件和数据流 |
| [控制面设计](architecture/control-plane-design.md) | etcd、自举、租约和治理 |
| [控制面取舍](architecture/control-plane-whitepaper.md) | 角色、存储和一致性取舍 |
| [控制面架构图](architecture/control-plane-architecture.drawio) | 控制面和 SoftKV 图示 |
| [流量面架构](architecture/traffic-plane-architecture.md) | 流量面边界和处理链路 |
| [流量面详细设计](design/traffic-plane-design.md) | 配置编译、转发和重试 |
| [SoftKV 设计](design/soft-kv-and-gossip.md) | 软状态存储和 Gossip |
| [API 与配置参考](reference/api-reference.md) | 接口、字段和启动参数 |

## 文档约定

- `Server` 和 `Agent` 表示节点角色。
- `etcd member` 表示 Raft 成员。
- `节点注册键` 表示 `/mesh/nodes/{id}`。
- `服务配置` 表示 `/mesh/services/{name}`。
- `SoftKV` 表示进程内软状态存储。
- `流量面` 表示 `internal/traffic` 运行时。

## 实现边界

当前版本不提供管理接口鉴权。
当前版本不接入 Envoy 或 xDS。
当前版本不劫持主机流量。
调用方必须显式声明监听端口。
