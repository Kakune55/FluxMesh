# FluxMesh SoftKV 设计

## 1. 范围

SoftKV 保存可丢失的短期状态。
当前调用方只写入节点系统指标。

SoftKV 不保存服务配置。
SoftKV 也不管理节点成员关系。
这些数据仍由 etcd 保存。

## 2. 组件

| 组件 | 代码位置 | 职责 |
| --- | --- | --- |
| Store | `internal/softkv/store.go` | 保存、读取、合并和过期条目 |
| Bus | `internal/softkv/bus.go` | 传递本地写入事件 |
| Writer | `internal/softkv/writer.go` | 组合本地写入和事件发布 |
| Memberlist | `internal/softkv/memberlist.go` | 扩散事件和交换完整状态 |
| GC | `internal/app/app.go` | 定时删除过期条目 |

Store 和 Bus 都在进程内存中。
进程退出后，数据和计数全部丢失。

## 3. 条目模型

```json
{
  "key": "metrics/nodes/server-1",
  "value": {},
  "source_id": "server-1",
  "seq": 12,
  "updated_at": 1786000000000,
  "expires_at": 1786000030000,
  "ingested_at": 1786000000100
}
```

| 字段 | 含义 |
| --- | --- |
| `key` | 逻辑键 |
| `value` | 任意 JSON 值 |
| `source_id` | 写入节点 |
| `seq` | 来源节点的进程内序号 |
| `updated_at` | 来源节点写入时间 |
| `expires_at` | 来源节点计算的过期时间 |
| `ingested_at` | 当前节点接收时间 |

时间字段使用 Unix 毫秒。
序号按 `source_id` 分别递增。

## 4. 本地写入

Writer 先调用 Store Put。
Put 成功后，本地读取立即可见。
Writer 随后向 Bus 发布事件。

Bus 默认缓冲区为 256。
发布默认最多等待 200 毫秒。
当前 Bus 在缓冲区满时立即返回错误。

发布失败不回滚本地写入。
Writer 在结果中返回 `PublishErr`。
应用只记录该错误。

## 5. 节点指标

应用每 10 秒获取一次系统指标。
首次采样发生在首个周期之后。
指标保留两位小数。

指标键格式如下：

```text
metrics/nodes/{node-id}
```

指标 TTL 为 30 秒。
GC 每 1 秒扫描一次过期条目。
Get 和 List 也会隐藏过期条目。

`GET /api/v1/nodes` 聚合节点指标。
节点基础信息仍来自 etcd。

## 6. Gossip 启动

每个节点启动一个 memberlist 实例。
默认绑定 `0.0.0.0:7946`。
广播地址使用节点 IP。

应用在启动时读取一次节点列表。
应用使用已有节点作为 Join 目标。
当前实现不会刷新该目标列表。

Join 失败不终止 memberlist。
节点先以单节点模式运行。
它按 5 至 60 秒退避重试 Join。

memberlist 创建失败时，应用切换到 Loopback。
Loopback 只消费本地 Bus 事件。
该模式不会自动恢复 Gossip。
恢复需要重启进程。

## 7. 扩散和状态交换

本地 Put 事件进入广播队列。
队列按 memberlist 规则重复发送。
远端节点收到消息后调用 Merge。

memberlist 还支持完整状态交换。
本节点通过 `LocalState` 导出有效条目。
接收节点通过 `MergeRemoteState` 合并条目。

该交换发生在 memberlist Push/Pull 流程。
SoftKV 没有独立的定时反熵任务。

## 8. 合并规则

Merge 先拒绝空键和过期条目。
之后按以下顺序比较：

1. 来源相同时，较大序号优先。
2. 更新时间不同，较新时间优先。
3. 来源不同，较大来源 ID 优先。
4. 其他情况使用较大序号。

来源节点重启后，序号从零开始。
新时间通常允许新条目覆盖旧条目。
该规则依赖节点时钟大致同步。

条目的过期时间来自来源节点。
时钟偏差会影响接收节点的 TTL。

## 9. 查询接口

`GET /api/v1/softkv` 返回有效条目。
`prefix` 参数执行字符串前缀过滤。

`GET /api/v1/softkv/{key}` 精确查询。
路径中的斜杠必须做 URL 编码。

`GET /api/v1/softkv/stats` 返回本地计数。
这些计数不是集群聚合结果。

## 10. 统计字段

| 字段 | 含义 |
| --- | --- |
| `put_total` | 本地 Put 次数 |
| `put_errors` | 本地 Put 错误数 |
| `get_total` | Get 次数 |
| `get_hits` | Get 命中数 |
| `get_misses` | Get 未命中数 |
| `list_total` | List 次数 |
| `merge_total` | Merge 次数 |
| `merge_accepted` | 接受的远端条目数 |
| `merge_rejected` | 拒绝的远端条目数 |
| `delete_expired_runs` | GC 扫描次数 |
| `delete_expired_keys` | GC 删除数 |
| `live_entries` | Store 内条目数 |
| `sources` | 已见来源数 |

`live_entries` 包含尚未由 GC 删除的条目。
它可能短暂包含已过期条目。

## 11. 故障语义

- 本地 Put 失败时，不发布事件。
- Bus 满时，本地值仍有效。
- Gossip 丢包时，节点可能短暂不同。
- 节点重启后，软状态全部丢失。
- 网络分区恢复后，memberlist 可交换状态。
- Loopback 降级后，不会自动恢复跨节点扩散。

SoftKV 只提供最终一致视图。
调用方不能依赖线性读取。

## 12. 安全边界

Gossip 当前没有加密和认证。
任意可访问端口的节点可能加入。
部署环境必须隔离 7946 端口。

SoftKV 值使用 JSON 编码扩散。
调用方不应写入凭据或密钥。
