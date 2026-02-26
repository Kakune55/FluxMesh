package model

type Node struct {
	ID           string  `json:"id"`
	IP           string  `json:"ip"`
	Version      string  `json:"version"`
	Role         string  `json:"role"`
	Status       string  `json:"status"`
	Load         int     `json:"load"`
	CPUUsage     float64 `json:"cpu_usage"`
	MemoryUsage  float64 `json:"memory_usage"`
	SystemLoad1m float64 `json:"system_load_1m"`
}
