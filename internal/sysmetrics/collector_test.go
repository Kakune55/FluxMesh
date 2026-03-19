package sysmetrics

import (
	"errors"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

func setCollectorDepsForTest(t *testing.T, cpuFn func(time.Duration, bool) ([]float64, error), memFn func() (*mem.VirtualMemoryStat, error), loadFn func() (*load.AvgStat, error)) {
	oldCPU := cpuPercentFn
	oldMem := virtualMemoryFn
	oldLoad := loadAvgFn

	cpuPercentFn = cpuFn
	virtualMemoryFn = memFn
	loadAvgFn = loadFn

	t.Cleanup(func() {
		cpuPercentFn = oldCPU
		virtualMemoryFn = oldMem
		loadAvgFn = oldLoad
	})
}

func TestCollectorCollectSuccess(t *testing.T) {
	setCollectorDepsForTest(
		t,
		func(_ time.Duration, _ bool) ([]float64, error) { return []float64{42.5}, nil },
		func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{UsedPercent: 66.6}, nil },
		func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 1.1, Load5: 2.2, Load15: 3.3}, nil },
	)

	c := NewCollector()
	s, err := c.Collect()
	if err != nil {
		t.Fatalf("collect failed: %v", err)
	}
	if s.CPUUsage != 42.5 || s.MemoryUsage != 66.6 || s.SystemLoad1m != 1.1 || s.SystemLoad5m != 2.2 || s.SystemLoad15m != 3.3 {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
}

func TestCollectorCollectErrors(t *testing.T) {
	t.Run("cpu error", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return nil, errors.New("cpu err") },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{UsedPercent: 10}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 1}, nil },
		)

		_, err := NewCollector().Collect()
		if err == nil || err.Error() != "cpu err" {
			t.Fatalf("expected cpu err, got %v", err)
		}
	})

	t.Run("memory error", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{10}, nil },
			func() (*mem.VirtualMemoryStat, error) { return nil, errors.New("mem err") },
			func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 1}, nil },
		)

		_, err := NewCollector().Collect()
		if err == nil || err.Error() != "mem err" {
			t.Fatalf("expected mem err, got %v", err)
		}
	})

	t.Run("load error", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{10}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{UsedPercent: 10}, nil },
			func() (*load.AvgStat, error) { return nil, errors.New("load err") },
		)

		_, err := NewCollector().Collect()
		if err == nil || err.Error() != "load err" {
			t.Fatalf("expected load err, got %v", err)
		}
	})
}

func TestReadCPUUsageClamp(t *testing.T) {
	c := NewCollector()

	t.Run("empty usage", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{}, nil },
		)
		got, err := c.readCPUUsage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 0 {
			t.Fatalf("expected 0, got %v", got)
		}
	})

	t.Run("negative usage", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{-5}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{}, nil },
		)
		got, err := c.readCPUUsage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 0 {
			t.Fatalf("expected clamp to 0, got %v", got)
		}
	})

	t.Run("over 100 usage", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{120}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{}, nil },
		)
		got, err := c.readCPUUsage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 100 {
			t.Fatalf("expected clamp to 100, got %v", got)
		}
	})
}

func TestReadMemoryUsageClamp(t *testing.T) {
	t.Run("negative used percent", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{1}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{UsedPercent: -3}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{}, nil },
		)
		got, err := readMemoryUsage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 0 {
			t.Fatalf("expected 0, got %v", got)
		}
	})

	t.Run("over 100 used percent", func(t *testing.T) {
		setCollectorDepsForTest(
			t,
			func(_ time.Duration, _ bool) ([]float64, error) { return []float64{1}, nil },
			func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{UsedPercent: 200}, nil },
			func() (*load.AvgStat, error) { return &load.AvgStat{}, nil },
		)
		got, err := readMemoryUsage()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 100 {
			t.Fatalf("expected 100, got %v", got)
		}
	})
}

func TestReadLoadAvg(t *testing.T) {
	setCollectorDepsForTest(
		t,
		func(_ time.Duration, _ bool) ([]float64, error) { return []float64{1}, nil },
		func() (*mem.VirtualMemoryStat, error) { return &mem.VirtualMemoryStat{}, nil },
		func() (*load.AvgStat, error) { return &load.AvgStat{Load1: 0.1, Load5: 0.2, Load15: 0.3}, nil },
	)

	l1, l5, l15, err := readLoadAvg()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l1 != 0.1 || l5 != 0.2 || l15 != 0.3 {
		t.Fatalf("unexpected load avg values: %v %v %v", l1, l5, l15)
	}
}
