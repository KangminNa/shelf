package stats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseStat(t *testing.T) {
	idle, total, ok := parseStat("cpu  100 0 50 800 50 0 0 0 0 0\ncpu0 1 2 3 4\n")
	if !ok || idle != 850 || total != 1000 {
		t.Fatalf("idle=%d total=%d ok=%v", idle, total, ok)
	}
	if _, _, ok := parseStat("garbage"); ok {
		t.Fatal("garbage must not parse")
	}
}

func TestParseMeminfo(t *testing.T) {
	total, avail, ok := parseMeminfo("MemTotal:  8000000 kB\nMemFree: 100 kB\nMemAvailable:  6000000 kB\n")
	if !ok || total != 8000000*1024 || avail != 6000000*1024 {
		t.Fatalf("%d %d %v", total, avail, ok)
	}
	if _, _, ok := parseMeminfo("MemTotal: 10 kB\n"); ok {
		t.Fatal("without MemAvailable we do not guess")
	}
}

func TestSamplerComputesCPUFromTwoSamples(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	write("stat", "cpu  100 0 0 900 0 0 0 0\n")
	write("loadavg", "0.42 0.38 0.31 1/200 1234\n")
	write("meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\n")
	write("uptime", "3600.5 100.0\n")

	s := NewProcSampler(dir, dir)
	if s.Now().HasCPU {
		t.Fatal("one sample is not enough for CPU")
	}
	write("stat", "cpu  150 0 0 950 0 0 0 0\n") // 100틱 중 50틱 바쁨
	s.sample()
	snap := s.Now()
	if !snap.HasCPU || snap.CPUPercent != 50 {
		t.Fatalf("cpu %.1f %v", snap.CPUPercent, snap.HasCPU)
	}
	if !snap.HasLoad || snap.Load1 != 0.42 || !snap.HasMem || snap.MemUsed != 750*1024 || snap.Uptime.Hours() != 1 {
		t.Fatalf("%+v", snap)
	}
	if !snap.HasDisk || snap.DiskTotal == 0 {
		t.Fatal("disk usage of a real path is readable")
	}
}

func TestMissingProcIsUnknownNotZero(t *testing.T) {
	snap := NewProcSampler(filepath.Join(t.TempDir(), "nope"), "/definitely/not/here").Now()
	if snap.HasCPU || snap.HasMem || snap.HasLoad || snap.HasDisk {
		t.Fatalf("unreadable values must be marked unknown: %+v", snap)
	}
}
