# FluxMesh 控制面 MVP 快速启动

## 1. 构建

```bash
go build -o fluxmesh ./cmd/fluxmesh
```

## 2. 单节点 Server

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

## 3. 双节点 1S1A

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

## 4. 三节点 3S（本机演示端口版）

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

## 5. Docker Compose 快速测试（推荐）

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

## 6. API 联调清单（当前版本）

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

### 6.3 节点驱逐

```bash
# 驱逐普通节点（agent 或非 leader server）
curl -s -X DELETE http://127.0.0.1:15000/api/v1/nodes/agent-1 | jq .

# 驱逐 leader 需要显式确认
curl -s -X DELETE 'http://127.0.0.1:15000/api/v1/nodes/server-1?force=true' | jq .
```

说明：未携带 `force=true` 驱逐 leader 会返回 `409 Conflict`。

## 7. 清理

```bash
docker compose --profile s1a down -v
docker compose --profile 3s down -v
```

若你希望继续使用非 root 运行容器，需要提前给挂载目录授权，例如：

```bash
mkdir -p ./data/docker/server-1 ./data/docker/server-2 ./data/docker/server-3
chmod -R 777 ./data/docker
```
