package session

import (
	"sort"
	"sync"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

type Record struct {
	Session                protocol.SessionInfo `json:"session"`
	Runtime                protocol.RuntimeInfo `json:"runtime"`
	PersistedSessionHandle string               `json:"persisted_session_handle,omitempty"`
	WrapperConnected       bool                 `json:"wrapper_connected"`
	UpdatedAt              time.Time            `json:"updated_at"`
}

type Registry struct {
	mu      sync.Mutex
	records map[string]Record
}

func NewRegistry() *Registry {
	return &Registry{records: make(map[string]Record)}
}

func (r *Registry) Ensure(sessionID, persistedHandle string) Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[sessionID]
	record.Session = protocol.SessionInfo{ID: sessionID}
	if persistedHandle != "" {
		record.PersistedSessionHandle = persistedHandle
	}
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record
}

func (r *Registry) SetConnected(sessionID string, runtime protocol.RuntimeInfo, persistedHandle string) Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.records[sessionID]
	record.Session = protocol.SessionInfo{ID: sessionID}
	record.Runtime = runtime
	if persistedHandle != "" {
		record.PersistedSessionHandle = persistedHandle
	}
	record.WrapperConnected = true
	record.UpdatedAt = time.Now().UTC()
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
