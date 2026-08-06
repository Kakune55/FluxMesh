# FluxMesh API 与配置参考

管理接口默认监听 `:15000`。
所有响应使用 JSON。
当前接口不提供认证和授权。

## 1. 通用行为

除 `/health` 外，不支持的方法返回 `405`。
`/health` 当前不限制请求方法。
参数或配置错误返回 `400`。
资源不存在返回 `404`。
版本冲突返回 `409`。
上游目标解析失败返回 `502`。

错误响应格式如下：

```json
{
  "error": "error message"
}
```

当前错误消息没有稳定错误码。
调用方应先判断 HTTP 状态码。

## 2. 健康与集群

### 2.1 `GET /health`

该接口只报告进程版本。
它不检查 etcd 多数派和流量监听。

```json
{
  "status": "UP",
  "version": "v0.1.0"
}
```

### 2.2 `GET /api/v1/cluster/status`

该接口查询当前 etcd endpoint。
响应字段如下：

| 字段 | 含义 |
| --- | --- |
| `endpoint` | 当前客户端使用的 endpoint |
| `cluster_id` | etcd 集群 ID |
| `current_member_id` | 响应节点的 member ID |
| `leader_id` | 当前 leader ID |
| `raft_term` | 当前任期 |
| `raft_index` | 当前日志索引 |
| `raft_applied_index` | 已应用日志索引 |
| `db_size` | etcd 数据库字节数 |
| `members` | 成员列表 |

成员角色为 `leader`、`follower` 或 `learner`。

## 3. 节点

### 3.1 `GET /api/v1/nodes`

该接口列出有效节点注册键。
接口会补充 etcd 角色。
接口还会聚合 SoftKV 节点指标。

节点字段如下：

| 字段 | 含义 |
| --- | --- |
| `id` | 节点 ID |
| `ip` | 节点广播地址 |
| `version` | 节点版本 |
| `node_status.mesh_role` | `server` 或 `agent` |
| `node_status.etcd_role` | etcd 角色 |
| `node_status.node_status` | 当前固定为 `Ready` |
| `sys_load` | 本节点看到的 SoftKV 指标 |

### 3.2 `GET /api/v1/nodes/{id}`

该接口返回节点注册键的原始值。
它不聚合 SoftKV 指标。

节点不存在时返回 `404`。

### 3.3 `DELETE /api/v1/nodes/{id}`

查询参数：

| 参数 | 必填 | 含义 |
| --- | --- | --- |
| `force` | 否 | 允许驱逐 leader |

Agent 驱逐只删除节点注册键。
Server 驱逐还会删除 etcd member。
leader 未带 `force=true` 时返回 `409`。

控制面先删除注册键。
之后才删除 etcd member。
第二步失败时，第一步不会回滚。

驱逐不会停止目标进程。
操作前应先停止该进程。

响应示例：

```json
{
  "node_id": "server-2",
  "node_role": "server",
  "node_deleted": true,
  "member_id": 123,
  "member_removed": true
}
```

## 4. 服务配置

服务配置保存在 `/mesh/services/`。
接口最多读取 1 MiB 请求体。
当前实现不检查剩余内容。

### 4.1 `POST /api/v1/services`

POST 创建或覆盖同名配置。
该接口不是“仅创建”操作。

可选请求头：

| 请求头 | 含义 |
| --- | --- |
| `Content-Type` | 使用 `application/json` |
| `X-Operator` | 写入 `updated_by` |

成功时返回 `201`。
响应包含补全后的请求配置。
该响应不含有效资源版本。
它也不含注册表写入的审计时间。
写入后应使用 GET 读取完整资源。

### 4.2 `GET /api/v1/services`

该接口列出全部服务配置。
每项包含当前 `resource_version`。

列表顺序由 etcd 键顺序决定。
调用方不应依赖该顺序。

### 4.3 `GET /api/v1/services/{name}`

该接口读取一个服务配置。
资源不存在时返回 `404`。

### 4.4 `PUT /api/v1/services/{name}`

PUT 使用 CAS 更新。
接口最多读取 1 MiB 请求体。

版本号可来自以下位置：

1. 查询参数 `resource_version`
2. 请求体字段 `resource_version`

查询参数优先。
版本号必须是正整数。
路径名称必须与请求体名称一致。
请求体名称也可以为空。

冲突响应示例：

```json
{
  "error": "service resource version conflict",
  "current_resource_version": 42,
  "current_config": {}
}
```

### 4.5 `DELETE /api/v1/services/{name}`

DELETE 不检查资源版本。
成功时返回：

```json
{
  "status": "deleted",
  "name": "payment-api"
}
```

资源不存在时返回 `404`。

## 5. 流量面

### 5.1 `GET /api/v1/traffic/plan`

该接口重新读取并编译服务配置。
它只返回 L7 HTTP 监听。
它不返回 TCP 和 UDP 绑定。

响应格式如下：

```json
{
  "listeners": [
    {
      "listener": {
        "addr": "0.0.0.0",
        "port": 18080
      },
      "routes": []
    }
  ]
}
```

编译成功不表示端口已绑定。
端口冲突只会出现在进程日志中。

### 5.2 `GET /api/v1/traffic/match`

该接口只模拟 L7 路由。

| 参数 | 必填 | 默认值 |
| --- | --- | --- |
| `addr` | 否 | `0.0.0.0` |
| `port` | 是 | 无 |
| `host` | 是 | 无 |
| `path` | 否 | `/` |

成功响应字段如下：

- `listener`
- `service_name`
- `destination`
- `resolved_destination`
- `path_prefix`

未匹配路由时返回 `404`。
目标不能解析时返回 `502`。

该接口使用新建的选择状态。
结果不代表下一个真实请求。

### 5.3 `GET /api/v1/traffic/stats`

该接口返回当前节点的 L7 统计。
TCP 和 UDP 不进入该统计。

```json
{
  "stats": {
    "requests_total": 10,
    "success_total": 9,
    "error_total": 1,
    "retry_attempts_total": 2,
    "relay_hit_total": 0,
    "total_latency_ns": 1000000,
    "relay_latency_ns": 0
  },
  "avg_latency_ms": 0.1
}
```

`success_total` 包含 4xx 响应。
采样率大于 1 时，计数是估算值。

## 6. SoftKV

SoftKV 接口返回当前节点的本地视图。
这些接口不聚合集群全部节点。

### 6.1 `GET /api/v1/softkv`

可选参数 `prefix` 按键前缀过滤。
响应按键升序排列。
过期条目不会返回。

### 6.2 `GET /api/v1/softkv/{key}`

该接口精确查询一个键。
路径中的斜杠必须编码。
键不存在或过期时返回 `404`。

### 6.3 `GET /api/v1/softkv/stats`

该接口返回本地 Store 计数。
字段说明见 [SoftKV 设计](../design/soft-kv-and-gossip.md)。

## 7. 服务配置字段

### 7.1 顶层字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 服务名 |
| `namespace` | 否 | 业务命名空间 |
| `version` | 否 | 业务版本 |
| `resource_version` | PUT 时必填 | etcd 修改版本 |
| `routes` | 是 | 至少一项 |
| `backend_groups` | 否 | 后端组 |
| `traffic_policy` | 是 | 至少声明监听端口 |

### 7.2 路由

| 字段 | 默认值 | 约束 |
| --- | --- | --- |
| `hosts` | `["*"]` | 元素不能为空 |
| `path_prefix` | 无 | 必须以 `/` 开始 |
| `destination` | 无 | 必填 |
| `weight` | `100` | 1 到 100 |

L4 TCP 和 UDP 只使用第一条路由。

### 7.3 后端组

| 字段 | 约束 |
| --- | --- |
| `backend_groups[].name` | 计划内全局唯一 |
| `targets` | 至少一项 |
| `targets[].addr` | `host:port` |
| `targets[].weight` | 默认 100，范围 1 到 100 |
| `targets[].tags.relay` | 标记 Relay 候选 |

### 7.4 流量策略

| 字段 | 默认值 | 范围或取值 |
| --- | --- | --- |
| `proxy.layer` | `l7-http` | `l7-http`、`l4-tcp`、`l4-udp` |
| `protocols` | 按代理层 | `http`、`tcp` 或 `udp` |
| `listener.addr` | `0.0.0.0` | IPv4 |
| `listener.port` | 无 | 1 到 65535 |
| `lb.strategy` | `load-first` | 内置值或已注册名称 |
| `retry.max_attempts` | 运行时为 1 | 整数 |
| `retry.budget_ratio` | 零值 | 浮点数 |
| `relay.max_hops` | 运行时为 2 | 整数，仅 L7 |
| `observability.metrics_sample_rate` | `1` | 1 到 10000 |
| `udp.dial_timeout_ms` | `2000` | 1 到 60000 |
| `udp.read_timeout_ms` | `2000` | 1 到 60000 |
| `udp.write_timeout_ms` | `2000` | 1 到 60000 |
| `udp.session_ttl_ms` | `30000` | 100 到 3600000 |
| `udp.max_packet_size` | `65535` | 512 到 65535 |

## 8. 启动参数

命令行参数覆盖对应环境变量。
空环境变量按未设置处理。

| 参数 | 环境变量 | 默认值 |
| --- | --- | --- |
| `--role` | `FLUXMESH_ROLE` | `agent` |
| `--cluster-state` | `FLUXMESH_CLUSTER_STATE` | `new` |
| `--node-id` | `FLUXMESH_NODE_ID` | 无 |
| `--ip` | `FLUXMESH_IP` | `auto` |
| `--version` | `FLUXMESH_VERSION` | `v0.1.0` |
| `--data-dir` | `FLUXMESH_DATA_DIR` | `./data` |
| `--admin-addr` | `FLUXMESH_ADMIN_ADDR` | `:15000` |
| `--client-listen-url` | `FLUXMESH_CLIENT_LISTEN_URL` | `http://0.0.0.0:2379` |
| `--client-advertise-url` | `FLUXMESH_CLIENT_ADVERTISE_URL` | 自动推导 |
| `--peer-listen-url` | `FLUXMESH_PEER_LISTEN_URL` | `http://0.0.0.0:2380` |
| `--peer-advertise-url` | `FLUXMESH_PEER_ADVERTISE_URL` | 自动推导 |
| `--seed-endpoints` | `FLUXMESH_SEED_ENDPOINTS` | 无 |
| `--lease-ttl` | `FLUXMESH_LEASE_TTL` | `10` |

`node-id` 始终必填。
Agent 必须配置种子地址。
existing Server 也必须配置种子地址。
租约 TTL 必须大于零。

`ip=auto` 只查找非回环 IPv4。
找不到地址时，应用启动失败。

整数环境变量格式错误时，配置使用默认值。
当前实现不会报告该格式错误。

## 9. 默认端口

| 端口 | 用途 |
| --- | --- |
| `15000/tcp` | 管理接口 |
| `2379/tcp` | etcd client |
| `2380/tcp` | etcd peer |
| `7946/tcp` | memberlist |
| `7946/udp` | memberlist |

只有 Server 监听 etcd 端口。
所有节点默认监听 Gossip 端口。
