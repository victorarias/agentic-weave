package runtime

import "time"

type Descriptor struct {
	Name               string
	Kind               string
	Command            string
	Args               []string
	Transport          string
	ResumeStrategy     string
	Capabilities       map[string]bool
	SupportsPermission bool
	StartupTimeout     time.Duration
}

func PiRPC() Descriptor {
	return Descriptor{
		Name:           "pi-rpc",
		Kind:           "pi",
		Command:        "pi",
		Args:           []string{"--mode", "rpc", "--no-session"},
		Transport:      "rpc",
		ResumeStrategy: "session-file",
		Capabilities: map[string]bool{
			"session_status":              true,
			"session_prompt":              true,
			"session_cancel":              true,
			"session_permission_response": true,
			"session_update":              true,
			"delivery_interrupt":          true,
			"delivery_queue":              true,
			"delivery_deliver_when_idle":  true,
			"delivery_follow_up":          true,
		},
		SupportsPermission: true,
		StartupTimeout:     15 * time.Second,
	}
}

func PiPTY() Descriptor {
	return Descriptor{
		Name:           "pi-pty",
		Kind:           "pi",
		Command:        "pi",
		Args:           nil,
		Transport:      "pty",
		ResumeStrategy: "session-file",
		Capabilities: map[string]bool{
			"session_status": true,
			"session_update": true,
			"pty_output":     true,
			"pty_input":      true,
			"pty_resize":     true,
		},
		SupportsPermission: false,
		StartupTimeout:     15 * time.Second,
	}
}
