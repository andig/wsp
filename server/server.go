package server

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/andig/wsp"
	"nhooyr.io/websocket"
)

// Server is a Reverse HTTP Proxy over WebSocket
// This is the Server part, Clients will offer websocket connections,
// those will be pooled to transfer HTTP Request and response
type Server struct {
	Config *Config

	// In pools, keep connections with WebSocket peers.
	pools map[PoolID]*Pool

	// A RWMutex is a reader/writer mutual exclusion lock,
	// and it is for exclusive control with pools operation.
	//
	// This is locked when reading and writing pools, the timing is when:
	// 1. (rw) registering websocket clients in /register endpoint
	// 2. (rw) remove empty pools which has no connections
	// 3. (r) dispatching connection from available pools to clients requests
	//
	// And then it is released after each process is completed.
	lock sync.RWMutex
	done chan struct{}

	// Through dispatcher channel it communicates between "server" thread and "dispatcher" thread.
	// "server" thread sends the value to this channel when accepting requests in the endpoint /requests,
	// and "dispatcher" thread reads this channel.
	dispatcher chan *ConnectionRequest
}

// ConnectionRequest is used to request a proxy connection from the dispatcher
type ConnectionRequest struct {
	id         PoolID
	connection chan *Connection
}

// NewServer return a new Server instance
func NewServer(config *Config) *Server {
	server := &Server{
		Config:     config,
		pools:      make(map[PoolID]*Pool),
		done:       make(chan struct{}),
		dispatcher: make(chan *ConnectionRequest),
	}
	return server
}

// Start Server HTTP server
func (s *Server) Start() {
	go func() {
	L:
		for {
			select {
			case <-s.done:
				break L
			case <-time.After(30 * time.Second):
				s.clean()
			}
		}
	}()

	// Dispatch connection from available pools to clients requests
	// in a separate thread from the server thread.
	go s.dispatchConnections()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := strings.Split(r.URL.Path, "/")
	switch segments[len(segments)-1] {
	case "register":
		s.Register(w, r)
	case "request":
		s.Request(w, r)
	case "status":
		s.status(w, r)
	default:
		http.NotFound(w, r)
	}
}

// clean removes empty Pools which has no connection.
// It is invoked every 5 sesconds and at shutdown.
func (s *Server) clean() {
	s.lock.Lock()
	defer s.lock.Unlock()

	idle := 0
	busy := 0

	for id, pool := range s.pools {
		if pool.IsEmpty() {
			log.Printf("Removing empty connection pool: %s", id)
			pool.Shutdown()
			delete(s.pools, id)
			continue
		}

		ps := pool.Size()
		idle += ps.Idle
		busy += ps.Busy
	}

	log.Printf("%d pools, %d idle, %d busy", len(s.pools), idle, busy)
}

func (s *Server) getPool(id PoolID) (*Pool, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	pool, ok := s.pools[id]
	return pool, ok
}

// Dispatch connection from available pools to clients requests
func (s *Server) dispatchConnections() {
	for {
		// Runs in an infinite loop and keeps receiving the value from the `server.dispatcher` channel
		// The operator <- is "receive operator", which expression blocks until a value is available.
		request, ok := <-s.dispatcher
		if !ok {
			// The value of `ok` is false if it is a zero value generated because the channel is closed an empty.
			// In this case, that means server shutdowns.
			break
		}

		// A timeout is set for each dispatch request.
		ctx, cancel := context.WithTimeout(context.Background(), s.Config.Timeout)
		defer cancel()

	L:
		for {
			select {
			case <-ctx.Done(): // The timeout elapses
				break L
			default: // Go through
			}

			pool, ok := s.getPool(request.id)
			if !ok {
				// No connection pool available
				break
			}

			// [2]: Verify that we can use this connection and take it.
			if connection := <-pool.idle; connection.Take() {
				request.connection <- connection
				break
			}
		}

		close(request.connection)
	}
}

// newRequest creates a new connection request
func newRequest(poolId PoolID) *ConnectionRequest {
	return &ConnectionRequest{
		id:         poolId,
		connection: make(chan *Connection),
	}
}

func (s *Server) Request(w http.ResponseWriter, r *http.Request) {
	// [1]: Receive requests to be proxied
	// Parse destination URL
	dstURL := r.Header.Get("X-PROXY-DESTINATION")
	if dstURL == "" {
		wsp.ProxyErrorf(w, "Missing X-PROXY-DESTINATION header")
		return
	}
	poolId := PoolID(r.Header.Get("X-PROXY-CLIENT"))
	if poolId == "" {
		wsp.ProxyErrorf(w, "Missing X-PROXY-CLIENT header")
		return
	}
	URL, err := url.Parse(dstURL)
	if err != nil {
		wsp.ProxyErrorf(w, "Unable to parse X-PROXY-DESTINATION header")
		return
	}
	r.URL = URL

	log.Printf("%s %s", r.Method, r.URL.String())

	if _, ok := s.getPool(poolId); !ok {
		wsp.ProxyErrorf(w, "No proxy available")
		return
	}

	// [2]: Take an WebSocket connection available from pools for relaying received requests.
	request := newRequest(poolId)
	// "Dispatcher" is running in a separate thread from the server by `go s.dispatchConnections()`.
	// It waits to receive requests to dispatch connection from available pools to clients requests.
	// https://github.com/hgsgtk/wsp/blob/ea4902a8e11f820268e52a6245092728efeffd7f/server/server.go#L93
	//
	// Notify request from handler to dispatcher through Server.dispatcher channel.
	s.dispatcher <- request
	// Dispatcher tries to find an available connection pool,
	// and it returns the connection through Server.connection channel.
	// https://github.com/hgsgtk/wsp/blob/ea4902a8e11f820268e52a6245092728efeffd7f/server/server.go#L189
	//
	// Here waiting for a result from dispatcher.
	connection := <-request.connection
	if connection == nil {
		// It means that dispatcher has set `nil` which is a system error case that is
		// not expected in the normal flow.
		wsp.ProxyErrorf(w, "Unable to get a proxy connection")
		return
	}

	// [3]: Send the request to the peer through the WebSocket connection.
	if err := connection.proxyRequest(w, r); err != nil {
		// An error occurred throw the connection away
		log.Println(err)
		connection.Close()

		// Try to return an error to the client
		// This might fail if response headers have already been sent
		wsp.ProxyError(w, err)
	}
}

// Request receives the WebSocket upgrade handshake request from wsp_client.
func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	acceptOptions := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	}

	ws, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		wsp.ProxyErrorf(w, "HTTP upgrade error: %v", err)
		return
	}
	ws.SetReadLimit(-1)

	// 2. Wait a greeting message from the peer and parse it
	// The first message should contains the remote Proxy name and size
	_, greeting, err := ws.Read(context.Background())
	if err != nil {
		wsp.ProxyErrorf(w, "Unable to read greeting message: %s", err)
		ws.CloseNow()
		return
	}

	// Parse the greeting message
	split := strings.Split(string(greeting), "_")
	id := PoolID(split[0])
	size, err := strconv.Atoi(split[1])
	if err != nil {
		wsp.ProxyErrorf(w, "Unable to parse greeting message: %s", err)
		ws.CloseNow()
		return
	}

	// 3. Register the connection into server pools.
	// s.lock is for exclusive control of pools operation.
	s.lock.Lock()
	defer s.lock.Unlock()

	pool, ok := s.pools[id]
	if !ok {
		pool = NewPool(s, id)
		s.pools[id] = pool
	}
	// update pool size
	pool.size = size

	// Add the WebSocket connection to the pool
	pool.Register(ws)
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("ok"))
}

// Shutdown stop the Server
func (s *Server) Shutdown() {
	close(s.done)
	close(s.dispatcher)
	for _, pool := range s.pools {
		pool.Shutdown()
	}
	s.clean()
}
