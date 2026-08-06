# FluxMesh 快速开始

本页使用当前仓库中的参数和接口。
命令默认在仓库根目录运行。

## 1. 环境

- Go 1.25 或更高版本
- `curl`
- `jq`
- Docker Compose，可选

## 2. 构建和测试

1. 构建二进制。

```bash
go build -o fluxmesh ./cmd/fluxmesh
```

2. 运行测试。

```bash
go test ./...
```

## 3. 启动单 Server

```bash
./fluxmesh \
  --role=server \
  --cluster-state=new \
  --node-id=server-1 \
  --ip=auto \
  --data-dir=./data \
  --client-listen-url=http://0.0.0.0:2379 \
  --peer-listen-url=http://0.0.0.0:2380 \
  --admin-addr=:15000
```

检查进程和节点注册：

```bash
curl -s http://127.0.0.1:15000/health | jq .
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
curl -s http://127.0.0.1:15000/api/v1/cluster/status | jq .
```

数据保存在 `./data/server-1`。

## 4. 加入一个 Agent

保持 Server 运行。
在另一个终端启动 Agent：

```bash
./fluxmesh \
  --role=agent \
  --node-id=agent-1 \
  --ip=auto \
  --seed-endpoints=http://127.0.0.1:2379 \
  --admin-addr=:15001
```

查询两个节点：

```bash
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
```

停止 Agent 后等待租约过期：

```bash
sleep 12
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
```

默认租约 TTL 为 10 秒。
实际删除时间会受 etcd 调度影响。

本机两个进程会争用 `7946` 端口。
后启动的进程会改用 Loopback。
这不影响 etcd 节点注册验证。
它会停止该进程的 Gossip 扩散。

## 5. 启动三个 Server

本机演示必须使用不同端口。
按顺序启动以下进程。

### 5.1 Server 1

```bash
./fluxmesh --role=server --cluster-state=new \
  --node-id=server-1 --ip=127.0.0.1 \
  --data-dir=./data \
  --client-listen-url=http://127.0.0.1:2379 \
  --client-advertise-url=http://127.0.0.1:2379 \
  --peer-listen-url=http://127.0.0.1:2380 \
  --peer-advertise-url=http://127.0.0.1:2380 \
  --admin-addr=:15000
```

### 5.2 Server 2

```bash
./fluxmesh --role=server --cluster-state=existing \
  --node-id=server-2 --ip=127.0.0.1 \
  --data-dir=./data \
  --client-listen-url=http://127.0.0.1:2479 \
  --client-advertise-url=http://127.0.0.1:2479 \
  --peer-listen-url=http://127.0.0.1:2480 \
  --peer-advertise-url=http://127.0.0.1:2480 \
  --seed-endpoints=http://127.0.0.1:2379 \
  --admin-addr=:15001
```

### 5.3 Server 3

```bash
./fluxmesh --role=server --cluster-state=existing \
  --node-id=server-3 --ip=127.0.0.1 \
  --data-dir=./data \
  --client-listen-url=http://127.0.0.1:3479 \
  --client-advertise-url=http://127.0.0.1:3479 \
  --peer-listen-url=http://127.0.0.1:3480 \
  --peer-advertise-url=http://127.0.0.1:3480 \
  --seed-endpoints=http://127.0.0.1:2379 \
  --admin-addr=:15002
```

检查成员和 leader：

```bash
curl -s http://127.0.0.1:15000/api/v1/cluster/status | jq .
```

本机三个进程也会争用 `7946` 端口。
该示例只验证 etcd 集群。
验证 Gossip 时，应使用容器或不同主机。

## 6. 使用 Docker Compose

### 6.1 启动 1S1A

```bash
docker compose --profile s1a up -d --build
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
```

Agent 管理接口映射到 `15001`。

### 6.2 启动 3S

```bash
docker compose --profile 3s up -d --build
curl -s http://127.0.0.1:15000/api/v1/nodes | jq .
curl -s http://127.0.0.1:15002/api/v1/nodes | jq .
curl -s http://127.0.0.1:15003/api/v1/nodes | jq .
```

### 6.3 清理容器

下面的命令删除 Compose 数据卷。

```bash
docker compose --profile s1a down -v
docker compose --profile 3s down -v
```

## 7. 创建 L7 服务配置

下面的配置监听 `18080`。
它把请求转发到 `127.0.0.1:18081`。

```bash
curl -s -X POST http://127.0.0.1:15000/api/v1/services \
  -H 'Content-Type: application/json' \
  -H 'X-Operator: demo-user' \
  -d '{
    "name": "payment-api",
    "routes": [
      {
        "hosts": ["pay.example.com"],
        "path_prefix": "/",
        "destination": "127.0.0.1:18081"
      }
    ],
    "traffic_policy": {
      "listener": {
        "addr": "0.0.0.0",
        "port": 18080
      }
    }
  }' | jq .
```

POST 使用覆盖语义。
同名配置会替换旧配置。

重新读取配置和版本：

```bash
curl -s http://127.0.0.1:15000/api/v1/services/payment-api | jq .
```

## 8. 验证流量计划

查看 L7 监听计划：

```bash
curl -s http://127.0.0.1:15000/api/v1/traffic/plan | jq .
```

模拟 Host 和 Path 匹配：

```bash
curl -s 'http://127.0.0.1:15000/api/v1/traffic/match?addr=0.0.0.0&port=18080&host=pay.example.com&path=/' | jq .
```

启动测试上游：

```bash
python3 -m http.server 18081 --bind 127.0.0.1
```

在另一个终端发送请求：

```bash
curl -i -H 'Host: pay.example.com' http://127.0.0.1:18080/
curl -s http://127.0.0.1:15000/api/v1/traffic/stats | jq .
```

## 9. 使用 CAS 更新

1. 获取当前版本。

```bash
rev=$(curl -s http://127.0.0.1:15000/api/v1/services/payment-api | jq -r .resource_version)
```

2. 携带该版本更新。

```bash
curl -s -X PUT "http://127.0.0.1:15000/api/v1/services/payment-api?resource_version=${rev}" \
  -H 'Content-Type: application/json' \
  -H 'X-Operator: demo-user' \
  -d '{
    "name": "payment-api",
    "routes": [
      {
        "hosts": ["pay.example.com"],
        "path_prefix": "/v2",
        "destination": "127.0.0.1:18082"
      }
    ],
    "traffic_policy": {
      "listener": {
        "addr": "0.0.0.0",
        "port": 18080
      }
    }
  }' | jq .
```

版本冲突时，接口返回 `409`。
响应包含当前版本和配置。

## 10. 查看 SoftKV

首次指标采样需要等待约 10 秒。

```bash
curl -s 'http://127.0.0.1:15000/api/v1/softkv?prefix=metrics/nodes/' | jq .
curl -s http://127.0.0.1:15000/api/v1/softkv/stats | jq .
```

精确键查询需要编码斜杠：

```bash
curl -s http://127.0.0.1:15000/api/v1/softkv/metrics%2Fnodes%2Fserver-1 | jq .
```

## 11. 驱逐节点

先停止目标进程。
否则目标可能重新注册。

驱逐 Agent：

```bash
curl -s -X DELETE http://127.0.0.1:15000/api/v1/nodes/agent-1 | jq .
```

驱逐 leader 会影响集群可用性。
确认多数派仍可用后再操作。

```bash
curl -s -X DELETE 'http://127.0.0.1:15000/api/v1/nodes/server-1?force=true' | jq .
```

## 12. 排查顺序

1. 检查 `/health`。
2. 检查 `/api/v1/cluster/status`。
3. 检查 `/api/v1/nodes`。
4. 检查 `/api/v1/services`。
5. 检查 `/api/v1/traffic/plan`。
6. 检查进程日志中的监听错误。
7. 检查 `/api/v1/softkv/stats`。
