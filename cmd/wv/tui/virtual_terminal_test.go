package tui

import (
	"io"
	"strings"
	"sync"
)

// VirtualTerminal is a deterministic in-memory terminal for tests.
type VirtualTerminal struct {
	mu sync.Mutex

	width  int
	writes []string

	input chan []byte
	raw   bool
}

func NewVirtualTerminal(width int) *VirtualTerminal {
	if width <= 0 {
		width = 80
	}
	return &VirtualTerminal{
		width: width,
		input: make(chan []byte, 64),
	}
}

func (v *VirtualTerminal) EnterRawMode() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.raw = true
	return nil
}

func (v *VirtualTerminal) ExitRawMode() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.raw = false
	return nil
}

func (v *VirtualTerminal) Width() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.width
}

func (v *VirtualTerminal) Read(data []byte) (int, error) {
	payload, ok := <-v.input
	if !ok {
		return 0, io.EOF
	}
	if len(payload) == 0 {
		return 0, nil
	}
	return copy(data, payload), nil
}

func (v *VirtualTerminal) Write(value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.writes = append(v.writes, value)
	return nil
}

func (v *VirtualTerminal) PushInput(data []byte) {
	if len(data) == 0 {
		return
	}
	payload := make([]byte, len(data))
	copy(payload, data)
	v.input <- payload
}

func (v *VirtualTerminal) PushInputString(data string) {
	v.PushInput([]byte(data))
}

func (v *VirtualTerminal) Resize(width int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if width > 0 {
		v.width = width
	}
}

func (v *VirtualTerminal) LastWrite() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.writes) == 0 {
		return ""
	}
	return v.writes[len(v.writes)-1]
}

func (v *VirtualTerminal) Output() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return strings.Join(v.writes, "")
}

func (v *VirtualTerminal) ResetOutput() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.writes = nil
}

func (v *VirtualTerminal) RawModeActive() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.raw
}
