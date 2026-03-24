# FluxMesh 快速开始

本页目标是让你尽快完成三件事：

1. 启动一个可用的 FluxMesh 节点
2. 验证控制面和流量面基础接口
3. 按场景扩展到 1S1A 或 3S

如果你想先理解项目整体结构，建议先看 [中文总入口](README.md)。

## 1. 适用范围

本页优先覆盖当前代码已经实现、并且适合直接联调的能力：

- 控制面自举与节点注册
- 服务配置 CRUD 与 CAS 更新
- SoftKV 查询与节点指标观测
- 流量面 plan/match/stats 验证

## 2. 构建

```bash
go build -o fluxmesh ./cmd/fluxmesh
```

## 3. 快速验证路径

如果你只想确认程序能跑通，按下面顺序执行：

1. 启动单节点 server
2. 访问 `/health`
3. 访问 `/api/v1/nodes`
4. 再看 `/api/v1/cluster/status`

## 4. 单节点 Server

```bash
./fluxmesh \
  --role=server \
  --cluster-state=new \
  --node-id=server-1 \
  --ip=auto \
  --data-dir=./data/server-1 \
  --client-listen-url=http://0.0.0.0:2379 \
  --peer-listen-url=http://0.0.0.0:2380 \
  --admin-addr=:15000
```

检查：

```bash
curl -s http://127.0.0.1:15000/health
curl -s http://127.0.0.1:15000/api/v1/nodes
```

说明：单节点场景主要用于确认二进制、参数、管理员端口和 embedded etcd 都能正常启动。

## 5. 双节点 1S1A

先启动 Server（同上），再启动 Agent：

```bash
./fluxmesh \
  --role=agent \
  --node-id=agent-1 \
  --ip=auto \
  --seed-endpoints=http://127.0.0.1:2379 \
  --admin-addr=:15001
```

检查：

```bash
curl -s http://127.0.0.1:15000/api/v1/nodes
curl -s http://127.0.0.1:15000/api/v1/nodes/agent-1
```

停止 Agent 后约 10 秒，再查询节点列表，`agent-1` 应自动消失。

这个模式适合验证“server + agent”的推荐最小拓扑，也是最接近低成本生产部署的配置。

## 6. 三节点 3S（本机演示端口版）

```bash
# server-1
./fluxmesh --role=server --cluster-state=new --node-id=server-1 --ip=127.0.0.1 \
  --client-listen-url=http://127.0.0.1:2379 --client-advertise-url=http://127.0.0.1:2379 \
  --peer-listen-url=http://127.0.0.1:2380 --peer-advertise-url=http://127.0.0.1:2380 --admin-addr=:15000 --data-dir=./data/server-1

# server-2
./fluxmesh --role=server --cluster-state=existing --node-id=server-2 --ip=127.0.0.1 \
  --client-listen-url=http://127.0.0.1:2479 --client-advertise-url=http://127.0.0.1:2479 \
  --peer-listen-url=http://127.0.0.1:2480 --peer-advertise-url=http://127.0.0.1:2480 --admin-addr=:15001 --data-dir=./data/server-2 \
  --seed-endpoints=http://127.0.0.1:2379

# server-3
./fluxmesh --role=server --cluster-state=existing --node-id=server-3 --ip=127.0.0.1 \
  --client-listen-url=http://127.0.0.1:3479 --client-advertise-url=http://127.0.0.1:3479 \
  --peer-listen-url=http://127.0.0.1:3480 --peer-advertise-url=http://127.0.0.1:3480 --admin-addr=:15002 --data-dir=./data/server-3 \
  --seed-endpoints=http://127.0.0.1:2379
```

检查：

```bash
curl -s http://127.0.0.1:15000/api/v1/nodes
```

这个模式适合验证 server 扩容、leader 选举和多节点拓扑展示。

## 7. Docker Compose 快速测试（推荐）

> 若你此前使用过旧镜像，请先执行 `docker compose down -v` 再 `--build` 重建，避免沿用旧容器用户导致权限错误。

### 5.1 启动 1S1A

```bash
docker compose --profile s1a up -d --build
curl -s http://127.0.0.1:15000/api/v1/nodes
```

停止 `agent-1` 验证自动收敛：

```bash
docker compose stop agent-1
sleep 12
curl -s http://127.0.0.1:15000/api/v1/nodes
```

### 5.2 启动 3S

```bash
docker compose --profile 3s up -d --build
curl -s http://127.0.0.1:15000/api/v1/nodes
curl -s http://127.0.0.1:15002/api/v1/nodes
curl -s http://127.0.0.1:15003/api/v1/nodes
```

说明：Compose 已配置 `server-1` 健康检查，`server-2/server-3` 会等待其健康后再启动；若瞬时网络抖动导致加入失败，`restart: on-failure` 会自动重试。

## 8. API 联调清单（当前版本）

如果你主要在验证接口契约，建议按下面的顺序看：

1. 诊断与拓扑
2. 服务配置
3. 流量面 plan/match/stats
4. SoftKV 观测
5. 节点驱逐

### 6.1 诊断与拓扑

```bash
curl -s http://127.0.0.1:15000/health | jq .
curl -s http://127.0.0.1:15000/api/v1/cluster/status | jq .
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
```

### 6.2 服务配置

```bash
curl -s -X POST http://127.0.0.1:15000/api/v1/services \
  -H 'Content-Type: application/json' \
  -H 'X-Operator: demo-user' \
  -d '{
    "name": "payment-svc",
    "namespace": "prod",
    "version": "v1",
    "routes": [
      {"path_prefix": "/", "destination": "payment-v1", "weight": 100}
    ]
  }' | jq .

curl -s http://127.0.0.1:15000/api/v1/services | jq .
curl -s http://127.0.0.1:15000/api/v1/services/payment-svc | jq .

# 从查询结果中取 resource_version 后执行 CAS 更新
rev=$(curl -s http://127.0.0.1:15000/api/v1/services/payment-svc | jq -r .resource_version)
curl -s -X PUT "http://127.0.0.1:15000/api/v1/services/payment-svc?resource_version=${rev}" \
  -H 'Content-Type: application/json' \
  -H 'X-Operator: demo-user' \
  -d '{
    "name": "payment-svc",
    "namespace": "prod",
    "version": "v2",
    "routes": [
      {"path_prefix": "/", "destination": "payment-v2", "weight": 100}
    ]
  }' | jq .

curl -s -X DELETE http://127.0.0.1:15000/api/v1/services/payment-svc | jq .
```

说明：若 CAS 冲突，接口会返回 `409` 且包含 `current_resource_version` 与 `current_config`，客户端可用该值直接重试或合并后重试。

### 6.3 流量面验证

```bash
curl -s http://127.0.0.1:15000/api/v1/traffic/plan | jq .

curl -s "http://127.0.0.1:15000/api/v1/traffic/match?addr=0.0.0.0&port=23306&host=example.com&path=/" | jq .

curl -s http://127.0.0.1:15000/api/v1/traffic/stats | jq .
```

建议先用 `/api/v1/traffic/plan` 看编译后的监听视图，再用 `/api/v1/traffic/match` 验证某个 host/path 会命中什么目标。

### 6.4 软状态观测（实验能力）

```bash
# 查询全部软状态
curl -s http://127.0.0.1:15000/api/v1/softkv | jq .

# 按前缀过滤（节点指标）
curl -s "http://127.0.0.1:15000/api/v1/softkv?prefix=metrics/nodes/" | jq .

# 按 key 精确查询（key 需 URL 编码）
curl -s "http://127.0.0.1:15000/api/v1/softkv/metrics%2Fnodes%2Fnode-1" | jq .

# 查询 softkv 运行统计（用于收敛/丢失趋势观测）
curl -s "http://127.0.0.1:15000/api/v1/softkv/stats" | jq .
```

### 6.5 节点驱逐

```bash
# 驱逐普通节点（agent 或非 leader server）
curl -s -X DELETE http://127.0.0.1:15000/api/v1/nodes/agent-1 | jq .

# 驱逐 leader 需要显式确认
curl -s -X DELETE 'http://127.0.0.1:15000/api/v1/nodes/server-1?force=true' | jq .
```

说明：未携带 `force=true` 驱逐 leader 会返回 `409 Conflict`。

## 9. 常见排查顺序

如果某个场景跑不通，建议按这个顺序排查：

1. 先看 `/health` 是否正常
2. 再看 `/api/v1/cluster/status` 是否已选出 leader
3. 再看 `/api/v1/nodes` 是否有节点注册
4. 再看 `/api/v1/services` 与 `/api/v1/traffic/plan`
5. 最后看 `/api/v1/softkv/stats` 是否有指标更新

## 10. 清理

```bash
docker compose --profile s1a down -v
docker compose --profile 3s down -v
```

若你希望继续使用非 root 运行容器，需要提前给挂载目录授权，例如：

```bash
mkdir -p ./data/docker/server-1 ./data/docker/server-2 ./data/docker/server-3
chmod -R 777 ./data/docker
```
