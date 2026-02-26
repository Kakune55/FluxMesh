package sysmetrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

type Snapshot struct {
	CPUUsage     float64
	MemoryUsage  float64
	SystemLoad1m float64
}

type Collector struct {
	mu        sync.Mutex
	prevIdle  uint64
	prevTotal uint64
	hasPrev   bool
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

	load1m, err := readLoad1m()
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		CPUUsage:     cpuUsage,
		MemoryUsage:  memoryUsage,
		SystemLoad1m: load1m,
	}, nil
}

func (c *Collector) readCPUUsage() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	idle, total, err := readCPUStat()
	if err != nil {
		return 0, err
	}

	if !c.hasPrev {
		c.prevIdle = idle
		c.prevTotal = total
		c.hasPrev = true
		return 0, nil
	}

	idleDelta := idle - c.prevIdle
	totalDelta := total - c.prevTotal

	c.prevIdle = idle
	c.prevTotal = total

	if totalDelta == 0 {
		return 0, nil
	}

	usage := (1 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	return usage, nil
}

func readCPUStat() (uint64, uint64, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0, 0, fmt.Errorf("failed to read /proc/stat")
	}

	fields := strings.Fields(scanner.Text())
	if len(fields) < 6 || fields[0] != "cpu" {
		return 0, 0, fmt.Errorf("unexpected /proc/stat format")
	}

	values := make([]uint64, 0, len(fields)-1)
	for _, item := range fields[1:] {
		v, convErr := strconv.ParseUint(item, 10, 64)
		if convErr != nil {
			return 0, 0, convErr
		}
		values = append(values, v)
	}

	var total uint64
	for _, v := range values {
		total += v
	}

	idle := values[3]
	if len(values) > 4 {
		idle += values[4]
	}

	return idle, total, nil
}

func readMemoryUsage() (float64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var memTotal uint64
	var memAvailable uint64

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			v, convErr := strconv.ParseUint(fields[1], 10, 64)
			if convErr == nil {
				memTotal = v
			}
		case "MemAvailable":
			v, convErr := strconv.ParseUint(fields[1], 10, 64)
			if convErr == nil {
				memAvailable = v
			}
		}
	}

	if memTotal == 0 {
		return 0, fmt.Errorf("invalid meminfo")
	}

	used := float64(memTotal-memAvailable) / float64(memTotal) * 100
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return used, nil
}

func readLoad1m() (float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid /proc/loadavg")
	}

	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}
