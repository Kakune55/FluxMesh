# FluxMesh 软状态 KV 与 Gossip 扩展设计方案

版本：draft-v1（2026-03）

## 1. 目标与背景

当前控制面把关键配置与拓扑信息放在 etcd（强一致）中，适合服务配置、成员治理等控制数据。

但有一类数据不适合持续写 etcd：

- 高频更新（如节点监控、瞬时负载）
- 可容忍短暂延迟
- 可容忍少量丢失
- 读写密集，以最终一致为主

本方案引入“软状态存储层（Soft State Store）”，使用内存 KV + Gossip 扩散，形成与 etcd 并行的弱一致状态面。

## 2. 设计原则

- 分层存储：强一致控制数据继续走 etcd，软状态走内存 KV。
- 最终一致：不追求每次写入全局强同步。
- 可丢可覆盖：新一轮采样可覆盖旧值。
- 本地优先：读本地内存，降低依赖链路与延迟。
- 反熵修复：周期摘要同步，修补丢包造成的不一致。

## 3. 适用与不适用数据

### 3.1 适用

- 节点 CPU/内存/Load 指标
- 临时健康评分
- 高频统计计数（QPS、错误率快照）

### 3.2 不适用

- 服务路由配置
- 节点角色与成员管理
- 需要审计与严格版本控制的控制命令

## 4. 架构概览

新增组件：

- SoftStore：本地内存 KV（带 TTL 与版本元数据）
- GossipBus：增量广播通道
- AntiEntropy：周期摘要对账与补发

数据流：

1. 本地采集器写入 SoftStore
2. Store 生成更新事件，进入广播队列
3. Gossip 扩散到其他节点
4. 远端节点按合并规则落本地 Store
5. 周期反熵任务修复遗漏

## 5. 数据模型建议

```go
type SoftEntry struct {
    Key       string
    Value     any
    SourceID  string
    Seq       uint64
    UpdatedAt int64
    ExpiresAt int64
}
```

字段语义：

- SourceID：产生该数据的节点 ID
- Seq：同 SourceID 下的单调递增序列
- UpdatedAt：UTC 毫秒时间戳（辅助排序）
- ExpiresAt：TTL 过期时间

## 6. 合并与冲突策略

推荐顺序：

1. 优先比较 SourceID 相同情况下的 Seq（更大者覆盖）
2. SourceID 不同且语义允许时，使用 UpdatedAt（LWW）
3. 若业务 key 要求按来源隔离，可使用 key=metric/{source}/{name}

说明：仅用时间戳会受时钟漂移影响，因此 SourceID+Seq 是主判据。

## 7. 协议与传播策略

### 7.1 增量广播

- 事件类型：Put/Expire
- 批处理窗口：100-300ms（减少广播风暴）
- 单消息大小限制：建议 < 8KB

### 7.2 反熵同步

- 周期：5-10 秒
- 方式：摘要对比（key + version）
- 差异补发：按缺失或版本落后补齐

## 8. 生命周期与资源控制

- TTL GC：每 1 秒回收过期项
- 最大键数：超过阈值时按过期优先 + 最久未更新淘汰
- 速率限制：对同 key 高频写做节流与覆盖

## 9. 接口建议（内部）

```go
type SoftStore interface {
    Put(ctx context.Context, key string, value any, ttl time.Duration, sourceID string) (SoftEntry, error)
    Get(ctx context.Context, key string) (SoftEntry, bool)
    List(ctx context.Context, prefix string) []SoftEntry
    Merge(ctx context.Context, entry SoftEntry) bool
}
```

说明：Merge 返回 bool，表示是否接受并更新本地状态，便于统计收敛率。

## 10. 与现有控制面的集成建议

第一阶段（最小可用）：

- 仅把节点指标写入 SoftStore
- API 读取优先 SoftStore，缺失回退 etcd
- 不改现有 etcd 指标写回逻辑（双写观察）

第二阶段（降负载）：

- 指标主路径迁到 SoftStore
- etcd 仅保留低频摘要字段

第三阶段（稳定优化）：

- 引入反熵摘要同步
- 增加收敛指标与丢包率观测

## 11. 故障场景与预期

- 单节点短时网络分区：本地读写不受影响，恢复后靠反熵收敛
- 广播丢包：下一轮增量或反熵补齐
- 节点重启：重新加入后同步热数据（非持久）

## 12. 风险与边界

- 软状态非持久，重启会丢；这是可接受前提。
- Gossip 扩散不是严格时序，消费者必须容忍乱序。
- 不应把策略配置等强一致数据误放入本层。

## 13. 验收标准

- 100ms-1s 内指标在多数节点可见
- 单次丢包不影响下一轮覆盖
- 10s 内跨节点指标收敛率达到预期阈值（如 >95%）
- etcd 写压显著下降（相较全量指标写入）

## 14. 开发任务拆分（建议）

1. 建立 SoftStore（map + TTL + merge）
2. 建立 GossipBus 抽象（先本地 mock）【已完成：loopback 事件总线】
3. 对接 sysmetrics 写入 SoftStore
4. 增加状态查询 API（仅观测）
5. 接入真实 memberlist 广播
6. 增加 AntiEntropy 与回归测试
