# FluxMesh API 与配置参考

本页是面向联调和排障的快速参考，不替代架构设计文档。若你想理解为什么这样设计，先看：

- [控制面白皮书](../architecture/control-plane-whitepaper.md)
- [流量面架构](../architecture/traffic-plane-architecture.md)
- [流量面详细设计](../design/traffic-plane-design.md)
- [软状态KV与Gossip设计方案](../design/soft-kv-and-gossip.md)

## 1. 管理面 API 总览

### 1.1 健康与诊断

#### GET /health

用途：进程存活和版本探针。

响应示例：

```json
{
  "status": "UP",
  "version": "v0.1.0"
}
```

#### GET /api/v1/cluster/status

用途：查看 etcd 集群和 Raft 状态。

常见字段：

- endpoint
- cluster_id
- current_member_id
- leader_id
- raft_term
- raft_index
- raft_applied_index
- db_size
- members

### 1.2 节点接口

#### GET /api/v1/nodes

用途：返回当前节点视图，节点负载会从 SoftKV 的 metrics/nodes/ 前缀补充。

#### GET /api/v1/nodes/:id

用途：查询单个节点详情。

#### DELETE /api/v1/nodes/:id

可选参数：

- force=true：允许驱逐 leader

行为：

- Agent 节点：删除节点注册键
- Server 节点：先删除节点注册键，再尝试 MemberRemove
- Leader 且未传 force=true：返回 409

### 1.3 服务配置接口

服务配置存储在 /mesh/services/，写入时会自动写入 `updated_at` 和 `updated_by`，读取时回填 `resource_version`。

#### POST /api/v1/services

用途：创建或覆盖服务配置。

请求头：

- Content-Type: application/json
- X-Operator: 可选，操作者标识

#### GET /api/v1/services

用途：列出所有服务配置。

#### GET /api/v1/services/:name

用途：读取指定服务配置。

#### PUT /api/v1/services/:name?resource_version=<rev>

用途：按 resource_version 做 CAS 更新。

规则：

- URL 参数优先
- 如果没有 URL 参数，则回退到请求体中的 resource_version
- 仍然没有版本号时返回 400

冲突返回：

- 409 Conflict
- current_resource_version
- current_config

#### DELETE /api/v1/services/:name

用途：删除指定服务配置。

## 2. 流量面 API 总览

### 2.1 计划与匹配

#### GET /api/v1/traffic/plan

用途：查看当前服务配置编译后的监听计划。

返回：

- listeners

每个 listener 包含：

- listener.addr
- listener.port
- routes[]

每个 route 包含：

- service_name
- hosts
- path_prefix
- destination
- weight

#### GET /api/v1/traffic/match?addr=&port=&host=&path=

用途：对指定监听上下文做路由模拟。

必填参数：

- port
- host

可选参数：

- addr：默认 0.0.0.0
- path：默认 /

成功返回字段：

- listener
- service_name
- destination
- resolved_destination
- path_prefix

错误场景：

- 400：端口非法或缺少 host
- 404：没有命中路由
- 502：destination 不能解析为可直连地址

#### GET /api/v1/traffic/stats

用途：查看流量面轻量统计。

返回字段：

- stats
- avg_latency_ms

stats 内部字段：

- requests_total
- success_total
- error_total
- retry_attempts_total
- relay_hit_total
- total_latency_ns
- relay_latency_ns

### 2.2 运行时行为摘要

- HTTP/L7 监听按 addr + port 合并
- L4/TCP 监听按 addr + port 单独绑定
- 同一监听不允许混用 L7 和 L4
- 路由优先级：精确 host 高于 *，长 path_prefix 高于短 path_prefix，weight 作为次级权重
- destination 可解析为 backend group、服务名回落、或直连地址

## 3. SoftKV API 总览

### 3.1 原始视图

#### GET /api/v1/softkv

用途：查看软状态条目。

可选参数：

- prefix：按前缀过滤

#### GET /api/v1/softkv/:key

用途：按 key 精确查询软状态。

说明：key 需要做 URL 编码。

#### GET /api/v1/softkv/stats

用途：查看 SoftKV 存储统计。

## 4. 服务配置模型速查

### 4.1 顶层字段

- name：必填
- namespace：可选
- version：可选
- routes：必填，至少 1 条
- backend_groups：可选
- traffic_policy：必填，至少包含 listener

### 4.2 traffic_policy 常用字段

- proxy.layer：l7-http 或 l4-tcp，默认 l7-http
- protocols：l7-http 只能是 http，l4-tcp 只能是 tcp
- listener.addr：默认 0.0.0.0
- listener.port：1 到 65535
- lb.strategy：load-first、round-robin、random、latency-first，或自定义 [a-z0-9-]
- retry.max_attempts：默认 1
- retry.budget_ratio：默认 1
- relay.max_hops：默认 2
- observability.metrics_sample_rate：默认 1，范围 1 到 10000

### 4.3 路由和后端组约束

- routes[].hosts 为空时默认补为 [*]
- routes[].weight 为空时默认 100
- backend_groups[].targets[].weight 为空时默认 100
- backend_groups 名称在全局 Plan 中必须唯一
- backend target.addr 必须是 host:port

## 5. 启动参数速查

### 5.1 基本参数

- --role：server 或 agent
- --cluster-state：new 或 existing，仅 server 需要
- --node-id：节点唯一 ID
- --ip：auto 或具体 IPv4
- --version：版本号
- --data-dir：embedded etcd 数据目录
- --admin-addr：管理面地址，默认 :15000
- --seed-endpoints：逗号分隔的 etcd 地址列表
- --lease-ttl：节点租约 TTL 秒数，默认 10

### 5.2 默认值说明

- role 默认 agent
- cluster-state 默认 new
- ip 默认 auto
- version 默认 v0.1.0
- data-dir 默认 ./data
- admin-addr 默认 :15000
- client-listen-url 默认 http://0.0.0.0:2379
- peer-listen-url 默认 http://0.0.0.0:2380
- lease-ttl 默认 10

### 5.3 环境变量

对应的环境变量前缀为 FLUXMESH_，例如：

- FLUXMESH_ROLE
- FLUXMESH_CLUSTER_STATE
- FLUXMESH_NODE_ID
- FLUXMESH_IP
- FLUXMESH_VERSION
- FLUXMESH_DATA_DIR
- FLUXMESH_ADMIN_ADDR
- FLUXMESH_CLIENT_LISTEN_URL
- FLUXMESH_CLIENT_ADVERTISE_URL
- FLUXMESH_PEER_LISTEN_URL
- FLUXMESH_PEER_ADVERTISE_URL
- FLUXMESH_SEED_ENDPOINTS
- FLUXMESH_LEASE_TTL

## 6. 推荐排障顺序

1. 先看 /health
2. 再看 /api/v1/cluster/status
3. 再看 /api/v1/nodes
4. 再看 /api/v1/services 和 /api/v1/traffic/plan
5. 最后看 /api/v1/softkv/stats