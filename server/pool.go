package server

import (
	"log"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Pool handles all connections from the peer.
type Pool struct {
	server *Server
	id     PoolID

	size int

	connections []*Connection
	idle        chan *Connection

	done bool
	lock sync.RWMutex
}

// PoolID represents the identifier of the connected WebSocket client.
type PoolID string

// NewPool creates a new Pool
func NewPool(server *Server, id PoolID) *Pool {
	p := new(Pool)
	p.server = server
	p.id = id
	p.idle = make(chan *Connection)
	return p
}

// Register creates a new Connection and adds it to the pool
func (pool *Pool) Register(ws *websocket.Conn) {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	// Ensure we never add a connection to a pool we have garbage collected
	if pool.done {
		return
	}

	log.Printf("Registering new connection from %s", pool.id)
	connection := NewConnection(pool, ws)
	pool.connections = append(pool.connections, connection)
}

// Offer offers an idle connection to the server.
func (pool *Pool) Offer(connection *Connection) {
	// The original code of root-gg/wsp was invoking goroutine,
	// but the callder was also invoking goroutine,
	// so it was deemed unnecessary and removed.
	pool.idle <- connection
}

// clean removes dead connection from the pool
// Look for dead connection in the pool
// This MUST be surrounded by pool.lock.Lock()
func (pool *Pool) clean() {
	idle := 0
	connections := make([]*Connection, 0, len(pool.connections))

	for _, connection := range pool.connections {
		// We need to be sure we'll never close a BUSY or soon to be BUSY connection
		connection.lock.Lock()
		if connection.status == Idle {
			idle++
			// We have enough idle connections in the pool.
			// Terminate the connection if it is idle since more that IdleTimeout
			if idle > pool.size && time.Since(connection.idleSince) > pool.server.Config.IdleTimeout {
				connection.close()
			}
		}
		if connection.status != Closed {
			connections = append(connections, connection)
		}
		connection.lock.Unlock()
	}
	pool.connections = connections
}

// IsEmpty clean the pool and return true if the pool is empty
func (pool *Pool) IsEmpty() bool {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	pool.clean()
	return len(pool.connections) == 0
}

// Shutdown closes every connections in the pool and cleans it
func (pool *Pool) Shutdown() {
	pool.lock.Lock()
	defer pool.lock.Unlock()

	pool.done = true
	for _, connection := range pool.connections {
		connection.Close()
	}

	pool.clean()
}

// PoolSize is the number of connection in each state in the pool
type PoolSize struct {
	Idle   int
	Busy   int
	Closed int
}

// Size return the number of connection in each state in the pool
func (pool *Pool) Size() *PoolSize {
	pool.lock.RLock()
	defer pool.lock.RUnlock()

	ps := new(PoolSize)
	for _, connection := range pool.connections {
		connection.lock.Lock()
		switch connection.status {
		case Idle:
			ps.Idle++
		case Busy:
			ps.Busy++
		case Closed:
			ps.Closed++
		}
		connection.lock.Unlock()
	}

	return ps
}
