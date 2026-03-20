# FluxMesh 流量面详细设计

版本：v1.0（2026-03-20）

## 1. 设计基线

本设计文档只描述当前代码已实现能力，字段与行为以以下模块为准：

- internal/model/service.go
- internal/registry/services.go
- internal/traffic/runtime.go
- internal/traffic/server.go
- internal/traffic/balancer.go
- internal/httpapi/server.go

## 2. 配置模型

### 2.1 服务顶层结构

ServiceConfig 关键字段：

- name
- namespace
- version
- routes[]
- backend_groups[]
- traffic_policy

说明：

- 资源版本 resource_version 由 etcd ModRevision 回填
- updated_at 与 updated_by 在写入时自动盖章

### 2.2 traffic_policy 字段

当前实现字段：

- proxy.layer
- protocols
- listener.addr
- listener.port
- lb.strategy
- retry.max_attempts
- retry.budget_ratio
- relay.max_hops
- observability.metrics_sample_rate

默认值（ApplyDefaults）：

- listener.addr = 0.0.0.0
- proxy.layer = l7-http
- protocols = [http]
- observability.metrics_sample_rate = 1
- routes[].hosts = [*]
- routes[].weight = 100
- backend_groups[].targets[].weight = 100

校验规则（Validate）：

- name 必填
- listener.port 范围为 1 到 65535
- listener.addr 必须是可绑定 IPv4
- proxy.layer 仅允许 l7-http 或 l4-tcp
- protocols 至少一个，且 MVP 仅允许 http
- lb.strategy 允许内置策略与别名，也允许匹配 [a-z0-9-] 的自定义名
- observability.metrics_sample_rate 范围为 1 到 10000
- routes 至少一个
- route.path_prefix 必须以 / 开头
- route.weight 范围为 1 到 100
- backend_group 名称在全局 Plan 中必须唯一
- backend target.addr 必须为 host:port
- backend target.weight 范围为 1 到 100

## 3. 路由编译与匹配

### 3.1 编译规则

BuildPlan 过程：

1. 对每个服务执行 defaults + validate。
2. 以 listener.addr + listener.port 作为监听合并键。
3. 将路由展平成 RouteBinding 并归并到监听维度。
4. 预构建 Host 匹配桶：exact host 与 wildcard host。
5. 为 backend_group 记录策略和 relay 候选索引。

### 3.2 匹配优先级

同监听上下文内：

1. Host 精确匹配优先于 *。
2. PathPrefix 长前缀优先于短前缀。
3. 路由权重更高者优先。

输出结果包含：

- listener
- service_name
- destination
- path_prefix

## 4. 目标解析与候选生成

### 4.1 destination 解析顺序

1. 命中 backend_groups[].name：按策略选目标。
2. 命中服务名：回落到该服务 listener 地址端口。
   说明：若 listener.addr 为 0.0.0.0，回落时转换为 127.0.0.1。
3. 命中直连地址：host:port 或 http(s) URL。

### 4.2 候选排序

对于 backend group：

- 先分组：direct targets 在前，relay targets 在后
- relay 识别：target.tags.relay 为 true/1/yes/on（大小写不敏感）
- 每组内按策略排序：
  - load-first：权重降序
  - round-robin：按组状态轮转
  - random：按权重随机
  - latency-first：当前与 load-first 一致（预留）

## 5. 转发与重试

### 5.1 单尝试路径

当有效尝试次数小于等于 1：

- 使用 direct transport 路径进行 RoundTrip
- 复写必要转发头
- 返回上游响应状态和 body

### 5.2 多尝试路径

当有效尝试次数大于 1：

- 先构造候选地址序列
- 读入请求体用于重放
- 对每个候选执行代理转发
- 状态码 >= 502 时可进入下一次重试
- 最后一跳返回最终结果

重试相关函数语义：

- effectiveMaxAttempts：未配置或小于等于 0 时为 1
- applyRetryBudget：
  - budget_ratio <= 0 时，最终尝试数强制为 1
  - 0 < budget_ratio < 1 时，按比例收敛额外重试次数
  - budget_ratio >= 1 时，允许 max_attempts 全量生效

## 6. Relay 与防环

请求头约定：

- X-FluxMesh-Hops：当前跳数
- X-FluxMesh-Service：命中服务
- X-FluxMesh-Destination：命中目标名
- X-FluxMesh-Upstream：最终上游地址

防环规则：

- max_hops 未配置时默认 2
- incoming_hops > max_hops 时返回 508
- 每次转发写入 hops+1

## 7. 运行时收敛

刷新周期：2 秒。

刷新行为：

- 拉取 services.List
- BuildPlan 编译失败时保留旧计划并告警
- 删除不再需要的监听并优雅关闭
- 启动新增监听
- 保留 Plan 内部轮询状态，避免每次刷新重置 RR 序列

## 8. 可观测性设计

统计项：

- requests_total
- success_total
- error_total
- retry_attempts_total
- relay_hit_total
- total_latency_ns
- relay_latency_ns

采样机制：

- 采样率来自 observability.metrics_sample_rate（服务级）
- 未配置时使用运行时默认采样率（初始为 1）
- retry 与 relay 事件可异步聚合，降低热路径开销

管理面接口：

- GET /api/v1/traffic/plan
- GET /api/v1/traffic/match
- GET /api/v1/traffic/stats

## 9. 已实现与未实现

已实现：

- 服务级监听声明与同端口复用
- Host + Path 路由匹配
- backend group + 直连 + 服务名回落三类 destination
- 负载策略插件化注册机制
- 重试预算与 relay 候选切换
- 运行时统计与管理 API

未实现或预留：

- 全局 traffic 配置层（当前仅服务级）
- 真正的 latency-first 指标驱动打分
- 非 HTTP 协议转发（l4-tcp 仅字段预留）
