# FluxMesh 控制面设计

## 1. 范围

控制面管理三类强一致状态：

- etcd member 元数据
- 节点注册键
- 服务配置

节点指标属于 SoftKV。
流量转发属于流量面。

## 2. 节点角色

### 2.1 Server

Server 启动内嵌 etcd。
Server 参与 Raft 选举。
Server 可创建新集群。
Server 也可加入现有集群。

### 2.2 Agent

Agent 不启动 etcd。
Agent 连接 `seed-endpoints`。
Agent 仍注册节点信息。
Agent 也运行管理接口和流量面。

## 3. etcd 启动

### 3.1 新集群

`cluster-state=new` 用于首个 Server。
应用使用节点 ID 作为 member 名称。
应用使用 peer 广播地址构造初始集群。

数据目录为：

```text
{data-dir}/{node-id}
```

### 3.2 加入现有集群

`cluster-state=existing` 需要种子地址。

1. 连接任一可用种子地址。
2. 调用 etcd `MemberAdd`。
3. 获取最新成员列表。
4. 构造 `initial-cluster`。
5. 启动本地内嵌 etcd。
6. 连接本地 client 广播地址。

本地 etcd 启动失败时，应用删除新 member。
删除失败时，协调器保存内存任务。
协调器按 2 至 32 秒退避重试。
进程退出后，任务不会持久保存。

### 3.3 广播地址

如果未指定广播地址，应用从监听地址推导。
应用把首个 `0.0.0.0` 替换为节点 IP。
显式地址适合多网卡和容器环境。

## 4. 节点注册

节点注册键格式如下：

```text
/mesh/nodes/{node-id}
```

节点值包含以下字段：

```json
{
  "id": "server-1",
  "ip": "10.0.0.11",
  "version": "v0.1.0",
  "node_status": {
    "mesh_role": "server",
    "node_status": "Ready"
  },
  "sys_load": {
    "cpu_usage": 0,
    "memory_usage": 0,
    "system_load_1m": 0
  }
}
```

应用先申请租约，再写入节点注册键。
默认租约 TTL 为 10 秒。
etcd SDK 持续发送 KeepAlive。

KeepAlive 通道关闭后，应用重建租约。
重试间隔从 1 秒增加到 8 秒。
新租约会重新写入节点注册键。

正常退出时，应用撤销当前租约。
异常退出时，etcd 在 TTL 到期后删键。

## 5. 节点视图

`GET /api/v1/nodes` 先读取节点注册键。
接口再查询 etcd 成员和 leader。
Server 的 `etcd_role` 为以下值：

- `leader`
- `follower`
- `unknown`

Agent 的 `etcd_role` 为 `agent`。
etcd 状态查询失败时，Server 显示 `unknown`。

接口最后读取 SoftKV 节点指标。
指标键格式为 `metrics/nodes/{node-id}`。
存在指标时，接口填充 `sys_load`。

单节点详情接口不填充 SoftKV 指标。
该接口返回注册键内的原始值。

## 6. 节点驱逐

Agent 驱逐只删除节点注册键。
Server 驱逐还会删除 etcd member。

驱逐 Server 时，控制面先查成员列表。
如果目标是 leader，接口默认返回 `409`。
调用方必须传入 `force=true`。

控制面先删除节点注册键。
控制面之后调用 `MemberRemove`。
如果删除 member 失败，注册键不会恢复。
调用方必须检查响应和集群状态。

驱逐不会停止目标进程。
仍在运行的节点可能重建租约。
运维系统应先停止目标进程。

## 7. 服务配置

服务配置键格式如下：

```text
/mesh/services/{name}
```

服务配置不绑定租约。
POST 使用 etcd Put 语义。
同名配置会直接覆盖。

写入前，服务注册表补全默认值。
服务注册表随后校验全部字段。
服务端写入 UTC 审计时间。
操作者默认值为 `api`。

GET 使用 etcd `ModRevision` 填充 `resource_version`。
存储值本身不保存该版本号。

## 8. CAS 更新

PUT 使用乐观并发控制。
查询参数中的版本号优先。
请求体版本号作为回退值。

etcd 事务比较当前 `ModRevision`。
版本一致时，事务写入配置。
版本不一致时，接口返回 `409`。
响应包含当前版本和当前配置。

DELETE 不使用 CAS。
并发删除和更新需要调用方协调。

## 9. 一致性与故障

Raft 写入需要多数派。
两个 Server 不能容忍一台故障。
双节点部署应使用 1S1A。
高可用部署应使用奇数个 Server。

租约只管理节点注册键。
租约不管理 etcd member。
Server 异常退出后，member 仍保留。
恢复或驱逐必须单独处理 member。

服务配置编译失败时，etcd 仍保留配置。
各节点流量面保留旧运行计划。
调用方应先使用匹配接口验证配置。

## 10. 安全边界

当前管理接口没有鉴权。
当前接口没有操作审计存储。
`updated_by` 由调用方提供。
该字段不能证明真实身份。

当前 etcd URL 默认使用 HTTP。
当前 Gossip 也不启用加密。
部署环境必须限制端口访问。
