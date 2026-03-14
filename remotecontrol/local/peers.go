package local

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/victorarias/agentic-weave/remotecontrol/protocol"
)

const peerWriteTimeout = time.Second

type peer interface {
	writeEnvelope(protocol.Envelope) error
	setInitialized(bool)
	initialized() bool
}

type localPeer struct {
	conn net.Conn
	mu   sync.Mutex
	init atomic.Bool
}

type relayPeer struct {
	conn *websocket.Conn
	mu   sync.Mutex
	init atomic.Bool
}

func (w *Wrapper) registerPeer(p peer) int64 {
	id := w.nextPeerID()
	w.peersMu.Lock()
	w.peers[id] = p
	w.peersMu.Unlock()
	return id
}

func (w *Wrapper) unregisterPeer(id int64) {
	w.peersMu.Lock()
	delete(w.peers, id)
	w.peersMu.Unlock()
}

func (w *Wrapper) snapshotInitializedPeers() []peer {
	w.peersMu.Lock()
	defer w.peersMu.Unlock()
	peers := make([]peer, 0, len(w.peers))
	for _, p := range w.peers {
		if p.initialized() {
			peers = append(peers, p)
		}
	}
	return peers
}

func (p *localPeer) writeEnvelope(env protocol.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(peerWriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})
	return protocol.WriteJSONLine(p.conn, env)
}

func (p *localPeer) setInitialized(v bool) { p.init.Store(v) }
func (p *localPeer) initialized() bool     { return p.init.Load() }

func (p *relayPeer) writeEnvelope(env protocol.Envelope) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(peerWriteTimeout))
	defer p.conn.SetWriteDeadline(time.Time{})
	return p.conn.WriteJSON(env)
}

func (p *relayPeer) setInitialized(v bool) { p.init.Store(v) }
func (p *relayPeer) initialized() bool     { return p.init.Load() }
