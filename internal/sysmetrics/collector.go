package sysmetrics

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

type Snapshot struct {
	CPUUsage      float64
	MemoryUsage   float64
	SystemLoad1m  float64
	SystemLoad5m  float64
	SystemLoad15m float64
}

type Collector struct {
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect() (Snapshot, error) {
	cpuUsage, err := c.readCPUUsage()
	if err != nil {
		return Snapshot{}, err
	}

	memoryUsage, err := readMemoryUsage()
	if err != nil {
		return Snapshot{}, err
	}

	load1m, load5m, load15m, err := readLoadAvg()
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		CPUUsage:      cpuUsage,
		MemoryUsage:   memoryUsage,
		SystemLoad1m:  load1m,
		SystemLoad5m:  load5m,
		SystemLoad15m: load15m,
	}, nil
}

func (c *Collector) readCPUUsage() (float64, error) {
	usages, err := cpu.Percent(0, false)
	if err != nil {
		return 0, err
	}
	if len(usages) == 0 {
		return 0, nil
	}

	usage := usages[0]
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	return usage, nil
}

func readMemoryUsage() (float64, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0, err
	}

	used := v.UsedPercent
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return used, nil
}

func readLoadAvg() (float64, float64, float64, error) {
	v, err := load.Avg()
	if err != nil {
		return 0, 0, 0, err
	}

	return v.Load1, v.Load5, v.Load15, nil
}
