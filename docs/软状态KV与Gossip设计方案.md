# FluxMesh SoftKV 架构白皮书（最终版）

版本：v1.0（2026-03-14）

## 1. 摘要

SoftKV 是 FluxMesh 控制面的软状态平面，用于承载高频、可丢、最终一致的数据（当前以节点监控指标为主）。

最终方案采用：

- 本地内存 KV（TTL + 合并规则）
- memberlist Gossip 增量扩散
- 管理面 API 聚合输出
- 强一致/弱一致分层边界

其中 etcd 继续承载强一致控制数据，SoftKV 不替代 etcd。

## 2. 设计目标

- 降低 etcd 写压：不再把高频 sys_load 周期回写到 etcd。
- 保持可观测性：`/api/v1/nodes` 仍可返回完整节点视图。
- 保持简洁实现：接口薄、链路短、失败可退化。
- 最终一致：允许短暂差异，不追求线性一致。

## 3. 设计边界

适用：

- 节点 CPU/内存/load 等高频监控快照
- 临时统计或可覆盖状态

不适用：

- 服务配置与路由策略
- 节点成员治理与角色变更
- 需要审计和强一致保障的数据

边界原则：

- 强一致控制数据 -> etcd
- 高频软状态数据 -> SoftKV

## 4. 组件与职责

### 4.1 Store

`internal/softkv/store.go`

- 负责本地 KV 存储
- `Put/Get/List/Merge/DeleteExpired`
- 支持 TTL 与过期回收

### 4.2 Bus

`internal/softkv/bus.go`

- 负责事件通道抽象（发布/订阅）
- 与存储解耦，便于测试与替换

### 4.3 Writer（写入封装）

`internal/softkv/writer.go`

- 一次调用完成 `Put + Publish`
- 保留“写入成功但广播失败”的可观测语义
- 业务侧调用更简洁

### 4.4 Memberlist 传输层

`internal/softkv/memberlist.go`

- Gossip 广播增量事件
- 接收远端事件并合并到本地 Store
- 广播不可用时支持降级回环（loopback）

## 5. 数据模型

SoftKV 条目核心字段：

- `key`: 逻辑键（例如 `metrics/nodes/server-1`）
- `value`: 任意值（当前为指标快照）
- `source_id`: 来源节点
- `seq`: 单来源递增版本
- `updated_at`: 更新时间（毫秒）
- `expires_at`: 过期时间（毫秒）

## 6. 一致性与合并规则

- 同 `source_id` 下，`seq` 更大者覆盖。
- 无序到达或重复消息通过 `Merge` 自然收敛。
- 以最终一致为目标，不保证严格时序。

## 7. 端到端工作流

### 7.1 写入链路（当前主路径）

1. 采集器在本地周期生成指标快照。
2. App 调用 `Writer.Write(...)` 写入 `metrics/nodes/{nodeID}`。
3. `Writer` 内部 `Put` 成功后尝试 `Publish`。
4. 本地节点可立即读到新值。

### 7.2 扩散链路

1. 事件进入 memberlist 广播队列。
2. 其他节点收到消息后调用 `Store.Merge`。
3. 各节点逐步收敛到相近视图。

### 7.3 读取链路

- `GET /api/v1/softkv`：原始软状态视图。
- `GET /api/v1/nodes`：
    - 基础节点信息来自 etcd。
    - `sys_load` 由 SoftKV 聚合填充。

## 8. 故障与退化策略

- Gossip 启动失败：回退 loopback，单节点仍可用。
- Publish 失败：不影响本地写入成功，错误可观测。
- 节点重启：软状态可丢，后续采样会重新覆盖。

## 9. 当前实现状态

已完成：

- SoftKV Store（TTL + Merge + GC）
- Bus 抽象与 Writer 薄封装
- memberlist 广播接入
- `/api/v1/softkv` 查询接口
- `/api/v1/nodes` 的 `sys_load` 迁移到 SoftKV
- 关键单测与集成验证

未完成（下一阶段）：

- 反熵同步（Anti-Entropy）
- SoftKV 专项可观测指标（丢包率/收敛延迟）
- 更细粒度流控与限速

## 10. 运维建议

- 使用三节点及以上部署 Gossip，避免单点视角偏差。
- 优先观察 `metrics/nodes/` 前缀键的更新与过期行为。
- 把 SoftKV 视为“实时快照层”，不要用于审计回溯。

## 11. 结论

SoftKV 最终版架构已形成稳定分层：

- etcd 负责“控制正确性”
- SoftKV 负责“状态实时性”

该分层在保持控制面一致性的同时，显著降低了高频监控数据对强一致存储的压力，并为后续反熵与更大规模扩展留出了演进空间。
