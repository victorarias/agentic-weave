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
	QueuedPrompts          int                          `json:"queued_prompts,omitempty"`
	PTYRows                int                          `json:"pty_rows,omitempty"`
	PTYCols                int                          `json:"pty_cols,omitempty"`
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
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record)
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
	record.QueuedPrompts = 0
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record)
}

func (r *Registry) SetDisconnected(sessionID, runtimeID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return cloneRecord(record), false
	}
	record.WrapperConnected = false
	record.Phase = ""
	record.Attachment = nil
	record.PendingPermissions = nil
	record.QueuedPrompts = 0
	record.PTYRows = 0
	record.PTYCols = 0
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
}

func (r *Registry) SetPhase(sessionID, runtimeID, phase string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return cloneRecord(record), false
	}
	record.Phase = phase
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
}

func (r *Registry) AddPermission(sessionID, runtimeID string, permission protocol.PermissionRequest) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return cloneRecord(record), false
	}
	updated := false
	for i := range record.PendingPermissions {
		if record.PendingPermissions[i].ID == permission.ID {
			record.PendingPermissions[i] = clonePermission(permission)
			updated = true
			break
		}
	}
	if !updated {
		record.PendingPermissions = append(record.PendingPermissions, clonePermission(permission))
	}
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
}

func (r *Registry) ResolvePermission(sessionID, runtimeID, requestID string) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if runtimeID != "" && record.Runtime.ID != "" && record.Runtime.ID != runtimeID {
		return cloneRecord(record), false
	}
	filtered := make([]protocol.PermissionRequest, 0, len(record.PendingPermissions))
	removed := false
	for _, permission := range record.PendingPermissions {
		if permission.ID == requestID {
			removed = true
			continue
		}
		filtered = append(filtered, clonePermission(permission))
	}
	if !removed {
		return cloneRecord(record), false
	}
	record.PendingPermissions = filtered
	record.State = deriveState(record.WrapperConnected, record.Phase, len(record.PendingPermissions))
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
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
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
}

func (r *Registry) SetQueuedPrompts(sessionID string, queued int) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if queued < 0 {
		queued = 0
	}
	record.QueuedPrompts = queued
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
}

func (r *Registry) SetPTYSize(sessionID string, rows, cols int) (Record, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[sessionID]
	if !ok {
		return Record{}, false
	}
	if rows > 0 {
		record.PTYRows = rows
	}
	if cols > 0 {
		record.PTYCols = cols
	}
	record.UpdatedAt = time.Now().UTC()
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
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
	r.records[sessionID] = cloneRecord(record)
	return cloneRecord(record), true
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
	return cloneRecord(record), ok
}

func (r *Registry) List() []Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Record, 0, len(r.records))
	for _, record := range r.records {
		out = append(out, cloneRecord(record))
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

func cloneRecord(record Record) Record {
	cloned := record
	if record.Attachment != nil {
		cloned.Attachment = &protocol.AttachmentInfo{ClientID: record.Attachment.ClientID, Mode: record.Attachment.Mode}
	}
	if len(record.PendingPermissions) > 0 {
		cloned.PendingPermissions = make([]protocol.PermissionRequest, len(record.PendingPermissions))
		for i, permission := range record.PendingPermissions {
			cloned.PendingPermissions[i] = clonePermission(permission)
		}
	}
	return cloned
}

func clonePermission(permission protocol.PermissionRequest) protocol.PermissionRequest {
	cloned := permission
	if len(permission.Options) > 0 {
		cloned.Options = append([]string(nil), permission.Options...)
	}
	if permission.Raw != nil {
		cloned.Raw = make(map[string]any, len(permission.Raw))
		for k, v := range permission.Raw {
			cloned.Raw[k] = v
		}
	}
	return cloned
}
