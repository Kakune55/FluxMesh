# FluxMesh 项目架构

## 1. 系统边界

FluxMesh 使用一个 Go 二进制。
每个节点运行同一套进程。
启动参数决定节点角色。

`Server` 内嵌 etcd。
`Agent` 只连接 etcd。
两种角色都运行管理接口。
两种角色都运行流量面和 SoftKV。

FluxMesh 当前不代理主机全部流量。
服务配置创建监听后，流量面接收请求。

## 2. 组件

| 组件 | 代码位置 | 职责 |
| --- | --- | --- |
| 进程编排 | `internal/app` | 启动组件并管理生命周期 |
| 配置 | `internal/config` | 读取参数和环境变量 |
| 内嵌 etcd | `internal/etcd` | 启动 Server 和加入集群 |
| 节点注册 | `internal/registry/nodes.go` | 注册、查询和驱逐节点 |
| 服务配置 | `internal/registry/services.go` | 保存配置并提供 CAS 更新 |
| 管理接口 | `internal/httpapi` | 提供诊断和管理 API |
| 流量面 | `internal/traffic` | 编译配置并代理流量 |
| SoftKV | `internal/softkv` | 保存并扩散软状态 |
| 系统指标 | `internal/sysmetrics` | 获取 CPU、内存和负载 |
| 回滚协调 | `internal/reconcile` | 重试清理失败的 etcd member |

## 3. 数据分层

| 数据 | 存储 | 一致性 | 生命周期 |
| --- | --- | --- | --- |
| etcd 成员 | etcd member 元数据 | Raft 一致 | 显式增加或删除 |
| 节点注册 | `/mesh/nodes/` | etcd 一致 | 绑定租约 |
| 服务配置 | `/mesh/services/` | etcd 一致 | 持久保存 |
| 节点指标 | SoftKV | 最终一致 | TTL 过期 |
| 流量统计 | 进程内原子计数 | 节点本地 | 进程退出后丢失 |

SoftKV 不替代 etcd。
服务配置不能写入 SoftKV。
节点指标不写入 etcd。

## 4. 启动链路

1. 配置模块校验启动参数。
2. 应用解析 `ip=auto`。
3. 节点连接或启动 etcd。
4. 节点注册键绑定租约。
5. 应用启动后台任务。
6. 流量面读取全部服务配置。
7. 管理接口开始监听。

后台任务包括租约恢复、指标采样、SoftKV 回收和 Gossip。
所有节点都启动 member 回滚协调器。
只有 joining Server 会添加回滚任务。

## 5. 配置变更链路

1. 调用方写入服务配置。
2. 服务注册表校验配置。
3. etcd 保存服务配置。
4. 流量面收到 Watch 事件。
5. `BuildPlan` 编译新计划。
6. 运行时增删流量监听。
7. 运行时替换当前计划。

Watch 断开后，流量面重新连接。
流量面每 30 秒执行一次兜底刷新。
编译失败时，旧计划继续生效。

## 6. 网络端口

| 默认端口 | 协议 | 用途 |
| --- | --- | --- |
| `15000` | TCP/HTTP | 管理接口 |
| `2379` | TCP/HTTP | etcd client |
| `2380` | TCP/HTTP | etcd peer |
| `7946` | TCP 和 UDP | memberlist Gossip |
| 服务配置端口 | TCP 或 UDP | 流量监听 |

Agent 不监听 etcd 端口。
Gossip 端口当前不能通过参数修改。

## 7. 运行约束

- 管理接口没有认证和授权。
- etcd 和 Gossip 默认不加密。
- 服务监听失败只写日志。
- 当前配置快照不会立即重试失败的监听。
- SoftKV 在进程重启后丢失。
- 流量统计只覆盖 L7 HTTP 请求。
- `latency-first` 只使用本节点观测值。

生产部署必须增加网络隔离。
生产部署还应增加认证和 TLS。
