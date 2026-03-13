package session

import (
	"sort"
	"sync"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

type Record struct {
	Session          protocol.SessionInfo `json:"session"`
	Runtime          protocol.RuntimeInfo `json:"runtime"`
	WrapperConnected bool                 `json:"wrapper_connected"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

type Registry struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewRegistry() *Registry {
	return &Registry{records: make(map[string]Record)}
}

func (r *Registry) SetConnected(sessionID string, runtime protocol.RuntimeInfo) Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := Record{
		Session:          protocol.SessionInfo{ID: sessionID},
		Runtime:          runtime,
		WrapperConnected: true,
		UpdatedAt:        time.Now().UTC(),
	}
	r.records[sessionID] = record
	return record
}

func (r *Registry) SetDisconnected(sessionID, runtimeID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return record, false
	}
	record.WrapperConnected = false
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) Get(sessionID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	return record, ok
}

func (r *Registry) List() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Session.ID < out[j].Session.ID
	})
	return out
}
