package session

import (
	"sort"
	"sync"
	"time"

	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

type Record struct {
	Session                protocol.SessionInfo         `json:"session"`
	Runtime                protocol.RuntimeInfo         `json:"runtime"`
	PersistedSessionHandle string                       `json:"persisted_session_handle,omitempty"`
	WrapperConnected       bool                         `json:"wrapper_connected"`
	State                  string                       `json:"state,omitempty"`
	Phase                  string                       `json:"phase,omitempty"`
	Attachment             *protocol.AttachmentInfo     `json:"attachment,omitempty"`
	PendingPermissions     []protocol.PermissionRequest `json:"pending_permissions,omitempty"`
	UpdatedAt              time.Time                    `json:"updated_at"`
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
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
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
	record.Phase = "running"
	record.PendingPermissions = nil
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
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
	record.Phase = ""
	record.Attachment = nil
	record.PendingPermissions = nil
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) SetPhase(sessionID, runtimeID, phase string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return record, false
	}
	record.Phase = phase
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) AddPermission(sessionID, runtimeID string, permission protocol.PermissionRequest) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return record, false
	}
	updated := false
	for i := range record.PendingPermissions {
		if record.PendingPermissions[i].ID == permission.ID {
			record.PendingPermissions[i] = permission
			updated = true
			break
		}
	}
	if !updated {
		record.PendingPermissions = append(record.PendingPermissions, permission)
	}
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) ResolvePermission(sessionID, runtimeID, requestID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return record, false
	}
	filtered := record.PendingPermissions[:0]
	removed := false
	for _, permission := range record.PendingPermissions {
		if permission.ID == requestID {
			removed = true
			continue
		}
		filtered = append(filtered, permission)
	}
	if !removed {
		return record, false
	}
	record.PendingPermissions = append([]protocol.PermissionRequest(nil), filtered...)
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) SetAttachment(sessionID string, attachment protocol.AttachmentInfo) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	record.Attachment = &protocol.AttachmentInfo{ClientID: attachment.ClientID, Mode: attachment.Mode}
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) ClearAttachment(sessionID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	record.Attachment = nil
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = record
	return record, true
}

func (r *Registry) HasPendingPermission(sessionID, requestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return false
	}
	for _, permission := range record.PendingPermissions {
		if permission.ID == requestID {
			return true
		}
	}
	return false
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

func deriveState(connected bool, phase string, pendingCount int) string {
	if !connected {
		return "stopped"
	}
	if pendingCount > 0 || phase == "waiting_permission" {
		return "waiting_permission"
	}
	if phase != "" {
		return phase
	}
	return "running"
}
