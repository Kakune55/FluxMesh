package model

type Node struct {
	ID      string  `json:"id"`
	IP      string  `json:"ip"`
	Version string  `json:"version"`
	Role    string  `json:"role"`
	Status  string  `json:"status"`
	Load    int     `json:"load"`
	SysLoad SysLoad `json:"sys_load"`
}

type SysLoad struct {
	CPUUsage     float64
	MemoryUsage  float64
	SystemLoad1m float64
}
