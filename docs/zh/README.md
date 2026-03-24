# FluxMesh 中文文档总入口

这是一份面向使用者和协作者的中文文档导航页。建议按“先上手、再理解架构、最后看实现细节”的顺序阅读。

## 推荐阅读顺序

1. [快速开始](getting-started.md)
2. [控制面白皮书](architecture/control-plane-whitepaper.md)
3. [控制面设计](architecture/control-plane-design.md)
4. [流量面架构](architecture/traffic-plane-architecture.md)
5. [流量面详细设计](design/traffic-plane-design.md)
6. [软状态KV与Gossip设计方案](design/soft-kv-and-gossip.md)
7. [API 与配置参考](reference/api-reference.md)

## 按用途查找

### 上手运行

- [快速开始](getting-started.md)

### 控制面理解

- [控制面白皮书](architecture/control-plane-whitepaper.md)
- [控制面设计](architecture/control-plane-design.md)
- [控制面架构图](architecture/control-plane-architecture.drawio)

### 流量面理解

- [流量面架构](architecture/traffic-plane-architecture.md)
- [流量面详细设计](design/traffic-plane-design.md)

### SoftKV 理解

- [软状态KV与Gossip设计方案](design/soft-kv-and-gossip.md)

### 参考页

- [API 与配置参考](reference/api-reference.md)

## 读文档时的建议

- 如果你是第一次接触项目，先看快速开始，再看控制面白皮书。
- 如果你关心运行时行为，优先看流量面架构和详细设计。
- 如果你关心高频指标与软状态收敛，先看 SoftKV 方案。
- 如果你想直接联调接口，快速开始里已经按场景整理了验证命令。

## 当前文档边界

- 这里的文档以当前代码实现为准。
- 已写明的 Future Work 只代表后续方向，不代表当前已实现。