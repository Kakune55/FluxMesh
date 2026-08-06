# FluxMesh 流量面详细设计

## 1. 服务配置

`ServiceConfig` 包含以下顶层字段：

- `name`
- `namespace`
- `version`
- `resource_version`
- `updated_at`
- `updated_by`
- `routes`
- `backend_groups`
- `traffic_policy`

`name`、`routes` 和监听端口必填。
服务注册表在写入前补全默认值。

## 2. 默认值

| 字段 | 默认值 |
| --- | --- |
| `proxy.layer` | `l7-http` |
| `protocols` | 按代理层选择 |
| `listener.addr` | `0.0.0.0` |
| `observability.metrics_sample_rate` | `1` |
| `udp.dial_timeout_ms` | `2000` |
| `udp.read_timeout_ms` | `2000` |
| `udp.write_timeout_ms` | `2000` |
| `udp.session_ttl_ms` | `30000` |
| `udp.max_packet_size` | `65535` |
| `routes[].hosts` | `["*"]` |
| `routes[].weight` | `100` |
| `targets[].weight` | `100` |

`protocols` 按代理层设置：

- `l7-http` 使用 `http`
- `l4-tcp` 使用 `tcp`
- `l4-udp` 使用 `udp`

`retry.max_attempts` 的运行默认值为 1。
`relay.max_hops` 的运行默认值为 2。
这两个默认值不会写回服务配置。

## 3. 校验

监听地址必须是 IPv4。
监听端口范围为 1 到 65535。
代理层只允许三种内置值。
协议必须与代理层匹配。

路由至少包含一项。
Path 前缀必须以 `/` 开始。
路由权重范围为 1 到 100。
目标不能为空。

后端组名在单个服务内不能重复。
计划内的后端组名必须全局唯一。
每个后端组至少包含一个目标。
目标地址必须使用 `host:port`。
目标权重范围为 1 到 100。

负载策略名只能使用小写字母、数字和连字符。
内置策略支持 `rr` 和 `rand` 别名。
自定义策略必须先在代码中注册。
否则目标解析会返回错误。

## 4. 计划编译

`BuildPlan` 先补全并校验每个服务。
监听键由地址和端口组成。

### 4.1 L7 HTTP

编译器把全部路由展平。
相同监听的路由合并处理。
编译器为精确 Host 建立索引。
编译器还建立通配 Host 索引。

L7 监听不能与 L4 TCP 共用监听键。
多个 L7 服务可以共用监听键。

### 4.2 L4 TCP

每个监听键只能绑定一个 TCP 服务。
TCP 绑定只使用 `routes[0].destination`。
其他路由不会参与 TCP 转发。

### 4.3 L4 UDP

每个监听键只能绑定一个 UDP 服务。
UDP 绑定只使用 `routes[0].destination`。
其他路由不会参与 UDP 转发。

UDP 使用独立套接字。
它可与 TCP 使用同一数字端口。

## 5. L7 匹配

运行时先规范化请求 Host。
规范化会转为小写并去掉端口。

匹配顺序如下：

1. 匹配精确 Host。
2. 匹配 `*` Host。
3. 选择最长 Path 前缀。
4. 选择更高路由权重。

如果分数仍相同，编译顺序决定结果。
编译器使用服务名和目标名稳定排序。

Path 匹配只使用字符串前缀。
`/api` 也会匹配 `/apiv2`。
配置方需要声明明确边界。

## 6. 目标解析

目标按以下顺序解析：

1. 查找同名后端组。
2. 查找同名服务。
3. 解析直连地址。

服务名回落到该服务监听。
`0.0.0.0` 会替换为 `127.0.0.1`。
该回落只适合本节点监听。

L7 直连目标支持以下形式：

- `host:port`
- `http://host:port/base`
- `https://host:port/base`

无协议地址在 L7 中使用 HTTP。
TCP 目标最终必须包含端口。
UDP URL 必须使用 `udp` 协议。

## 7. 后端选择

运行时先分离直接目标和 Relay 目标。
直接目标始终排在 Relay 目标之前。

以下标签值表示 Relay：

- `true`
- `1`
- `yes`
- `on`

标签名和值不区分大小写。

### 7.1 `load-first`

该策略按权重降序排列。
权重相同时按地址排序。

### 7.2 `round-robin`

该策略轮转候选起始位置。
刷新计划时保留轮询状态。

### 7.3 `random`

单次选择按权重随机。
多次尝试会随机打乱候选。

### 7.4 `latency-first`

该策略使用本节点时延 EWMA。
已有观测的目标排在前面。
同类目标再按权重和地址排序。

TCP 只记录建连时延。
UDP 记录单次往返时延。
L7 记录非重试响应的转发时延。

## 8. 重试预算

`max_attempts` 包含首次尝试。
小于等于零时按 1 处理。

`budget_ratio <= 0` 时只尝试一次。
`budget_ratio >= 1` 时使用全部次数。
中间值按额外次数向下取整。
如果结果为零，仍允许一次重试。

### 8.1 L7 HTTP

多次尝试会缓存完整请求体。
当前实现没有缓存大小上限。
运行时不会判断 HTTP 方法。

上游状态码不小于 502 时重试。
最后一次响应总会返回调用方。
4xx 和 5xx 以下状态不会重试。

### 8.2 L4 TCP

TCP 只重试解析和建连失败。
建连成功后开始双向复制。
复制中断不会重新连接。

### 8.3 L4 UDP

UDP 在读写或建连失败后换目标。
每个候选执行一次请求响应往返。
无响应会等到读取超时。

## 9. Relay 和跳数

Relay 标签只改变候选顺序和统计。
它不会自动发现中继路径。
目标地址必须指向可用代理节点。

L7 使用 `X-FluxMesh-Hops` 防环。
非法或负数值返回 `400`。
输入跳数大于上限时返回 `508`。
等于上限时仍会转发一次。

每次 L7 转发写入以下请求头：

- `X-FluxMesh-Service`
- `X-FluxMesh-Destination`
- `X-FluxMesh-Upstream`
- `X-FluxMesh-Hops`

TCP 和 UDP 不处理这些请求头。
`relay.max_hops` 不限制 L4 转发。

## 10. 监听收敛

启动时，流量面同步编译配置。
编译失败会终止应用启动。

运行时使用 etcd Watch 触发刷新。
每 30 秒还有一次兜底刷新。
服务名和版本未变化时跳过刷新。

删除配置会关闭对应监听。
HTTP 监听最多等待 3 秒关闭。
TCP 和 UDP 监听直接关闭套接字。

新增监听失败只记录日志。
当前快照仍会标记为已处理。
修改配置后才会重新尝试绑定。

## 11. L7 统计

统计接口只覆盖 L7 HTTP。
采样率 1 表示记录全部请求。
更高采样率按权重估算总量。

| 字段 | 含义 |
| --- | --- |
| `requests_total` | 估算请求数 |
| `error_total` | 状态码不小于 500 的请求数 |
| `success_total` | 请求数减错误数 |
| `retry_attempts_total` | 首次尝试之外的尝试数 |
| `relay_hit_total` | 使用过 Relay 候选的请求数 |
| `total_latency_ns` | 请求总耗时 |
| `relay_latency_ns` | Relay 请求总耗时 |

`success_total` 包含 4xx 响应。
异步事件队列满时，重试和 Relay 统计会丢失。

## 12. 配置示例

### 12.1 L7 HTTP

```json
{
  "name": "payment-api",
  "routes": [
    {
      "hosts": ["pay.example.com"],
      "path_prefix": "/",
      "destination": "127.0.0.1:8080"
    }
  ],
  "traffic_policy": {
    "listener": {
      "addr": "0.0.0.0",
      "port": 18080
    }
  }
}
```

### 12.2 L4 TCP

```json
{
  "name": "mysql-gateway",
  "routes": [
    {
      "path_prefix": "/",
      "destination": "10.10.0.12:3306"
    }
  ],
  "traffic_policy": {
    "proxy": {"layer": "l4-tcp"},
    "protocols": ["tcp"],
    "listener": {
      "addr": "0.0.0.0",
      "port": 23306
    }
  }
}
```

### 12.3 L4 UDP

```json
{
  "name": "dns-gateway",
  "routes": [
    {
      "path_prefix": "/",
      "destination": "1.1.1.1:53"
    }
  ],
  "traffic_policy": {
    "proxy": {"layer": "l4-udp"},
    "protocols": ["udp"],
    "listener": {
      "addr": "0.0.0.0",
      "port": 1053
    },
    "udp": {
      "read_timeout_ms": 2000,
      "session_ttl_ms": 30000
    }
  }
}
```
