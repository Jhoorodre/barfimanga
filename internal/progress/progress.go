package progress

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

type ProgressTracker struct {
	Total     int64
	Done      atomic.Int64
	FromCache atomic.Int64
}

func (t *ProgressTracker) Increment() {
	t.Done.Add(1)
}

type Progress interface {
	Start(total int64, tracker *ProgressTracker)
	Finish(success bool)
}

func NewProgress(quiet bool) Progress {
	if quiet {
		return noopProgress{}
	}
	// Use stderr to not pollute stdout piping
	if isatty.IsTerminal(os.Stderr.Fd()) {
		return newBarProgress()
	}
	return newPlainProgress()
}

// -- Noop Progress --
type noopProgress struct{}

func (p noopProgress) Start(total int64, tracker *ProgressTracker) {}
func (p noopProgress) Finish(success bool)                         {}

// -- Plain Progress --
type plainProgress struct {
	tracker *ProgressTracker
	ticker  *time.Ticker
	done    chan struct{}
}

func newPlainProgress() *plainProgress {
	return &plainProgress{}
}

func (p *plainProgress) Start(total int64, tracker *ProgressTracker) {
	p.tracker = tracker
	p.ticker = time.NewTicker(2 * time.Second)
	p.done = make(chan struct{})

	go func() {
		for {
			select {
			case <-p.done:
				return
			case <-p.ticker.C:
				cached := p.tracker.FromCache.Load()
				done := p.tracker.Done.Load()
				fmt.Fprintf(os.Stderr, "Progresso: %d/%d (cache:%d upload:%d)\n", done, p.tracker.Total, cached, done-cached)
			}
		}
	}()
}

func (p *plainProgress) Finish(success bool) {
	if p.ticker != nil {
		p.ticker.Stop()
		close(p.done)
	}
	cached := p.tracker.FromCache.Load()
	done := p.tracker.Done.Load()
	fmt.Fprintf(os.Stderr, "Finalizado: %d/%d (cache:%d upload:%d)\n", done, p.tracker.Total, cached, done-cached)
}

// -- Bar Progress --
type barProgress struct {
	tracker *ProgressTracker
	ticker  *time.Ticker
	done    chan struct{}
	prog    progress.Model
}

func newBarProgress() *barProgress {
	prog := progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(),
	)
	return &barProgress{prog: prog}
}

func (p *barProgress) Start(total int64, tracker *ProgressTracker) {
	p.tracker = tracker
	p.ticker = time.NewTicker(100 * time.Millisecond)
	p.done = make(chan struct{})

	go func() {
		for {
			select {
			case <-p.done:
				return
			case <-p.ticker.C:
				p.render()
			}
		}
	}()
}

func (p *barProgress) render() {
	done := p.tracker.Done.Load()
	cached := p.tracker.FromCache.Load()
	total := p.tracker.Total
	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total)
	}

	width, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || width < 10 {
		width = 80
	}

	// "c:119|u:4 38/65 " → ~18 chars; barra preenche o resto
	info := fmt.Sprintf(" c:%d|u:%d %d/%d ", cached, done-cached, done, total)
	p.prog.Width = width - len(info)
	if p.prog.Width < 10 {
		p.prog.Width = 10
	}

	barStr := p.prog.ViewAs(percent)
	fmt.Fprintf(os.Stderr, "\r\033[K%s%s", barStr, info)
}

func (p *barProgress) Finish(success bool) {
	if p.ticker != nil {
		p.ticker.Stop()
		close(p.done)
	}
	p.render()
	fmt.Fprintln(os.Stderr)
}
