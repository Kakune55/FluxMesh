# FluxMesh 流量面架构文档

版本：v1.0（2026-03-20）

## 1. 文档范围

本文档描述 FluxMesh 当前代码已实现的流量面架构。内容以 internal/traffic、internal/model、internal/httpapi、internal/registry 的现状为准。

目标：

- 明确流量面的运行时组件与职责边界
- 明确与控制面数据源和管理接口的关系
- 明确请求在节点内的处理路径

非目标：

- 不描述尚未实现的 eBPF/iptables 劫持
- 不描述 xDS/Envoy 对接
- 不描述 gRPC 数据转发

## 2. 总体架构

FluxMesh 流量面采用“每节点本地运行时 + 控制面配置驱动”的模式。

- 配置来源：etcd 中 /mesh/services/ 前缀下的 ServiceConfig
- 运行时进程：internal/traffic.Server
- 规划编译器：internal/traffic.BuildPlan
- 管理与诊断入口：internal/httpapi

关键特性：

- 监听自动收敛：按 listener.addr + listener.port 合并复用监听（HTTP），并维护独立 TCP 绑定
- Host + Path 匹配：同监听下多服务路由统一编排
- 后端解析：支持 backend_groups、服务名回落、直连地址
- 策略化选路：load-first、round-robin、random（latency-first 已预留）
- 失败恢复：重试尝试次数 + 预算比例 + 中继候选兜底
- 低开销统计：原子计数 + 采样聚合

## 3. 组件分层

### 3.1 配置与模型层

- ServiceConfig 模型定义位于 internal/model/service.go
- 服务配置存储位于 internal/registry/services.go

职责：

- ApplyDefaults 填充最小可运行默认值
- Validate 执行服务级字段合法性校验
- Put/UpdateWithRevision 在写入 etcd 前强制 defaults + validate

### 3.2 规划层（Plan）

- 入口：traffic.BuildPlan
- 输出：Plan（监听视图、路由匹配结构、目标解析索引、策略状态）

职责：

- 将多个服务编译为按监听键合并的 HTTP 路由表
- 生成 L4-TCP 监听绑定（一个监听绑定一个 TCP 服务）
- 预构建 Host/Path 匹配结构（exact + wildcard）
- 维护 backend group 到策略和 relay 目标索引
- 检测监听冲突：同 addr+port 禁止混用 l7-http 与 l4-tcp

### 3.3 运行层（Runtime Server）

- 入口：traffic.Server.Start
- 周期：每 2 秒刷新一次服务配置并收敛监听

职责：

- 启停 HTTP/TCP 监听并保持与 Plan 一致
- 处理 HTTP 请求匹配、目标解析、转发和重试
- 处理 TCP 连接透传与上游重连尝试
- 执行 hop 限制与 relay 判断
- 统计请求级与转发级指标

### 3.4 管理 API 层

- /api/v1/traffic/plan：查看编译后的监听与路由结果
- /api/v1/traffic/match：对指定 addr/port/host/path 做匹配与解析
- /api/v1/traffic/stats：查看运行时统计与平均时延

## 4. 数据流与控制流

### 4.1 配置下发流

1. 运维通过 /api/v1/services 写入服务配置。
2. registry.Services 将配置持久化到 /mesh/services/。
3. traffic.Server 周期读取 services.List 并调用 BuildPlan。
4. runtime 对比 desired listener 集合，增删监听并原子替换 Plan。

### 4.2 请求处理流

HTTP 路径：

1. 请求进入某个监听地址端口。
2. 按 host + path 在当前 Plan 中匹配路由。
3. 按 destination 解析目标：backend_group -> service listener -> host:port/http(s) URL。
4. 根据 retry 和 budget 计算尝试次数，按策略生成候选。
5. 逐个候选转发，成功则返回，失败按条件重试。
6. 写入统计与 relay 相关指标。

TCP 路径：

1. 连接进入某个 TCP 监听地址端口。
2. 按监听键获取唯一 TCP 绑定（service + destination）。
3. 按 retry 规则生成上游候选并依次拨号。
4. 建立连接后双向透传字节流（client <-> upstream）。

### 4.3 中继判定流

- relay 目标来源于 backend target 的 tags.relay=true/1/yes/on。
- 请求携带 X-FluxMesh-Hops，超过 max_hops 返回 508。
- 转发时自动递增 hops 头部。

## 5. 与控制面的边界

强一致配置（etcd）：

- 服务路由规则
- 监听参数
- 负载均衡策略选择
- 重试与 relay 策略参数

软状态（SoftKV）：

- 当前流量面核心转发路径未直接消费 SoftKV 指标做动态打分。
- latency-first 策略当前以 load-first 行为兜底，后续可接入实时指标。

## 6. 可用性与性能取向

已落地的工程取向：

- 单尝试主路径采用 direct transport fast-path，减少通用代理链路开销
- 监听和目标对象缓存，避免重复构建
- 指标采样与异步聚合，降低请求路径写放大
- 重试体缓存与复用，保证重试语义一致

当前约束：

- L4-TCP 为基础透传模式，不支持基于 SNI/应用层特征路由
- latency-first 仍为预留语义，未实现独立时延评分
- relay 不做全局路径搜索，仅基于后端组候选顺序切换

## 7. 演进建议

- 接入 SoftKV 运行态指标，落地 latency-first 实际打分
- 在 relay 场景引入更细粒度路径选择与惩罚策略
- 增加分级可观测性（按服务/路由维度）并控制高基数标签
