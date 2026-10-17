package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
)

type progress struct {
	sync.Mutex
	total int
	done  int
	unit  string
	start time.Time
}

func newProgress(total int, unit string) *progress {
	p := &progress{total: total, unit: unit, start: time.Now()}
	p.draw()
	return p
}

func (p *progress) println(line string) {
	if tty {
		fmt.Print("\r\033[K")
	}
	fmt.Println(line)
	p.draw()
}

func (p *progress) draw() {
	if !tty || p.total == 0 {
		return
	}
	width := 32
	if cols, _, err := term.GetSize(os.Stdout.Fd()); err == nil {
		width = min(width, max(cols-44, 8))
	}
	filled := p.done * width / p.total
	bar := good.Render(strings.Repeat("━", filled)) + faint.Render(strings.Repeat("━", width-filled))
	elapsed := faint.Render(fmt.Sprintf("%.1fs", time.Since(p.start).Seconds()))
	fmt.Printf("\r\033[K  %s  %d/%d %s  %s", bar, p.done, p.total, p.unit, elapsed)
}

func (p *progress) finish() {
	if tty {
		fmt.Print("\r\033[K")
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
