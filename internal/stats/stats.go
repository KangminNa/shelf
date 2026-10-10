// Package stats는 서버 한 대의 CPU·메모리·디스크를 읽는다.
// 화면 요청이 기다리지 않도록 뒤에서 주기적으로 표본을 뜨고, 화면은 마지막 표본만 읽는다.
// 읽을 수 없는 값은 추측하지 않고 비워 둔다 (화면에는 "—").
package stats

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KangminNa/naru/internal/model"
)

// ProcSampler는 /proc을 읽어 표본을 뜬다. 화면은 마지막 표본만 읽으므로 기다리지 않는다.
type ProcSampler struct {
	proc string // /proc 위치 — 테스트에서 바꾼다
	disk string // 디스크 여유를 잴 경로 (데이터 디렉터리)

	mu        sync.RWMutex
	last      model.ServerSnapshot
	prevIdle  uint64
	prevTotal uint64
}

func NewProcSampler(procRoot, diskPath string) *ProcSampler {
	s := &ProcSampler{proc: procRoot, disk: diskPath}
	s.sample()
	return s
}

// Run은 ctx가 끝날 때까지 every마다 표본을 뜬다.
func (s *ProcSampler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sample()
		}
	}
}

func (s *ProcSampler) Now() model.ServerSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

func (s *ProcSampler) sample() {
	snap := model.ServerSnapshot{Cores: runtime.NumCPU()}

	if text, err := os.ReadFile(filepath.Join(s.proc, "stat")); err == nil {
		if idle, total, ok := parseStat(string(text)); ok {
			s.mu.RLock()
			prevIdle, prevTotal := s.prevIdle, s.prevTotal
			s.mu.RUnlock()
			if prevTotal > 0 && total > prevTotal {
				busy := float64((total-prevTotal)-(idle-prevIdle)) / float64(total-prevTotal)
				snap.CPUPercent = busy * 100
				snap.HasCPU = true
			}
			s.mu.Lock()
			s.prevIdle, s.prevTotal = idle, total
			s.mu.Unlock()
		}
	}
	if text, err := os.ReadFile(filepath.Join(s.proc, "loadavg")); err == nil {
		snap.Load1, snap.HasLoad = parseLoad(string(text))
	}
	if text, err := os.ReadFile(filepath.Join(s.proc, "meminfo")); err == nil {
		if total, avail, ok := parseMeminfo(string(text)); ok {
			snap.MemTotal, snap.MemUsed, snap.HasMem = total, total-avail, true
		}
	}
	if text, err := os.ReadFile(filepath.Join(s.proc, "uptime")); err == nil {
		if f := strings.Fields(string(text)); len(f) > 0 {
			if sec, err := strconv.ParseFloat(f[0], 64); err == nil {
				snap.Uptime = time.Duration(sec) * time.Second
			}
		}
	}
	if total, free, ok := diskUsage(s.disk); ok {
		snap.DiskTotal, snap.DiskFree, snap.HasDisk = total, free, true
	}

	s.mu.Lock()
	s.last = snap
	s.mu.Unlock()
}

// parseStat은 /proc/stat 첫 줄에서 유휴 시간과 전체 시간을 꺼낸다.
func parseStat(text string) (idle, total uint64, ok bool) {
	line, _, _ := strings.Cut(text, "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0, 0, false
	}
	for i, v := range f[1:] {
		if i >= 8 { // user nice system idle iowait irq softirq steal — guest는 user에 이미 들어 있다
			break
		}
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += n
		if i == 3 || i == 4 { // idle, iowait
			idle += n
		}
	}
	return idle, total, true
}

func parseLoad(text string) (float64, bool) {
	f := strings.Fields(text)
	if len(f) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	return v, err == nil
}

// parseMeminfo는 전체와 "실제로 쓸 수 있는" 메모리(MemAvailable)를 바이트로 돌려준다.
func parseMeminfo(text string) (total, avail uint64, ok bool) {
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total, haveTotal = n*1024, true
		case "MemAvailable:":
			avail, haveAvail = n*1024, true
		}
	}
	return total, avail, haveTotal && haveAvail && avail <= total
}
