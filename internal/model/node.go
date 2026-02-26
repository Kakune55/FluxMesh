package model

type Node struct {
	ID      string  `json:"id"`
	IP      string  `json:"ip"`
	Version string  `json:"version"`
	NodeStatus NodeStatus `json:"node_status"`
	SysLoad SysLoad `json:"sys_load"`
}

type NodeStatus struct {
	NodeRole    string  `json:"mesh_role"`
	EtcdRole    string  `json:"etcd_role,omitempty"`
	NodeStatus  string  `json:"node_status"`
}

type SysLoad struct {
	CPUUsage     float64 `json:"cpu_usage"`
	MemoryUsage  float64 `json:"memory_usage"`
	SystemLoad1m float64 `json:"system_load_1m"`
	SystemLoad5m float64 `json:"system_load_5m,omitempty"`
	SystemLoad15m float64 `json:"system_load_15m,omitempty"`
}
