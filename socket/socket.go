// Socket package provides a concise, powerful and high-performance TCP socket.
//
// Copyright 2017 HenryLee. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
package socket

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/goutil/errors"
	"github.com/henrylee2cn/teleport/codec"
)

type (
	// Socket is a generic stream-oriented network connection.
	//
	// Multiple goroutines may invoke methods on a Socket simultaneously.
	Socket interface {
		// LocalAddr returns the local network address.
		LocalAddr() net.Addr

		// RemoteAddr returns the remote network address.
		RemoteAddr() net.Addr

		// SetDeadline sets the read and write deadlines associated
		// with the connection. It is equivalent to calling both
		// SetReadDeadline and SetWriteDeadline.
		//
		// A deadline is an absolute time after which I/O operations
		// fail with a timeout (see type Error) instead of
		// blocking. The deadline applies to all future and pending
		// I/O, not just the immediately following call to Read or
		// Write. After a deadline has been exceeded, the connection
		// can be refreshed by setting a deadline in the future.
		//
		// An idle timeout can be implemented by repeatedly extending
		// the deadline after successful Read or Write calls.
		//
		// A zero value for t means I/O operations will not time out.
		SetDeadline(t time.Time) error

		// SetReadDeadline sets the deadline for future Read calls
		// and any currently-blocked Read call.
		// A zero value for t means Read will not time out.
		SetReadDeadline(t time.Time) error

		// SetWriteDeadline sets the deadline for future Write calls
		// and any currently-blocked Write call.
		// Even if write times out, it may return n > 0, indicating that
		// some of the data was successfully written.
		// A zero value for t means Write will not time out.
		SetWriteDeadline(t time.Time) error

		// WritePacket writes header and body to the connection.
		// Note: must be safe for concurrent use by multiple goroutines.
		WritePacket(packet *Packet) error

		// ReadPacket reads header and body from the connection.
		// Note: must be safe for concurrent use by multiple goroutines.
		ReadPacket(packet *Packet) error

		// Read reads data from the connection.
		// Read can be made to time out and return an Error with Timeout() == true
		// after a fixed time limit; see SetDeadline and SetReadDeadline.
		Read(b []byte) (n int, err error)

		// Write writes data to the connection.
		// Write can be made to time out and return an Error with Timeout() == true
		// after a fixed time limit; see SetDeadline and SetWriteDeadline.
		Write(b []byte) (n int, err error)

		// Reset reset net.Conn
		// Reset(net.Conn)

		// Close closes the connection socket.
		// Any blocked Read or Write operations will be unblocked and return errors.
		Close() error

		// Public returns temporary public data of Socket.
		Public() goutil.Map
		// PublicLen returns the length of public data of Socket.
		PublicLen() int
		// Id returns the socket id.
		Id() string
		// SetId sets the socket id.
		SetId(string)
	}
	socket struct {
		net.Conn
		protocol  Proto
		id        string
		idMutex   sync.RWMutex
		ctxPublic goutil.Map
		mu        sync.Mutex
		curState  int32
		fromPool  bool
	}
)

const (
	normal      int32 = 0
	activeClose int32 = 1
)

var _ net.Conn = Socket(nil)

// ErrProactivelyCloseSocket proactively close the socket error.
var ErrProactivelyCloseSocket = errors.New("socket is closed proactively")

// GetSocket gets a Socket from pool, and reset it.
func GetSocket(c net.Conn, protoFunc ...ProtoFunc) Socket {
	s := socketPool.Get().(*socket)
	s.Reset(c, protoFunc...)
	return s
}

var socketPool = sync.Pool{
	New: func() interface{} {
		s := newSocket(nil, nil)
		s.fromPool = true
		return s
	},
}

// NewSocket wraps a net.Conn as a Socket.
func NewSocket(c net.Conn, protoFunc ...ProtoFunc) Socket {
	return newSocket(c, protoFunc)
}

func newSocket(c net.Conn, protoFuncs []ProtoFunc) *socket {
	var s = &socket{
		protocol: getProto(protoFuncs, c),
		Conn:     c,
	}
	return s
}

// WritePacket writes header and body to the connection.
// WritePacket can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetWriteDeadline.
// Note:
//  For the byte stream type of body, write directly, do not do any processing;
//  Must be safe for concurrent use by multiple goroutines.
func (s *socket) WritePacket(packet *Packet) (err error) {
	if packet.BodyType == codec.NilCodecId {
		packet.BodyType = defaultBodyType.Id()
	}
	defer func() {
		if p := recover(); p != nil {
			err = errors.Errorf("Write bad packet: %v\nstack: %s", p, goutil.PanicTrace(1))
		} else if err != nil && s.isActiveClosed() {
			err = ErrProactivelyCloseSocket
		}
	}()
	return s.protocol.Pack(packet)
}

// ReadPacket reads header and body from the connection.
// Note:
//  For the byte stream type of body, read directly, do not do any processing;
//  Must be safe for concurrent use by multiple goroutines.
func (s *socket) ReadPacket(packet *Packet) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = errors.Errorf("Read bad packet: %v\nstack: %s", p, goutil.PanicTrace(1))
		}
	}()
	return s.protocol.Unpack(packet)
}

// Public returns temporary public data of Socket.
func (s *socket) Public() goutil.Map {
	if s.ctxPublic == nil {
		s.ctxPublic = goutil.RwMap()
	}
	return s.ctxPublic
}

// PublicLen returns the length of public data of Socket.
func (s *socket) PublicLen() int {
	if s.ctxPublic == nil {
		return 0
	}
	return s.ctxPublic.Len()
}

// Id returns the socket id.
func (s *socket) Id() string {
	s.idMutex.RLock()
	id := s.id
	if len(id) == 0 {
		id = s.RemoteAddr().String()
	}
	s.idMutex.RUnlock()
	return id
}

// SetId sets the socket id.
func (s *socket) SetId(id string) {
	s.idMutex.Lock()
	s.id = id
	s.idMutex.Unlock()
}

// Reset reset net.Conn
func (s *socket) Reset(netConn net.Conn, protoFunc ...ProtoFunc) {
	atomic.StoreInt32(&s.curState, activeClose)
	if s.Conn != nil {
		s.Conn.Close()
	}
	s.mu.Lock()
	s.Conn = netConn
	s.SetId("")
	s.protocol = getProto(protoFunc, netConn)
	atomic.StoreInt32(&s.curState, normal)
	s.mu.Unlock()
}

// Close closes the connection socket.
// Any blocked Read or Write operations will be unblocked and return errors.
// If it is from 'GetSocket()' function(a pool), return itself to pool.
func (s *socket) Close() error {
	if s.isActiveClosed() {
		return nil
	}
	atomic.StoreInt32(&s.curState, activeClose)

	var errs []error
	if s.Conn != nil {
		errs = append(errs, s.Conn.Close())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isActiveClosed() {
		return nil
	}

	if s.fromPool {
		s.Conn = nil
		s.ctxPublic = nil
		s.protocol = nil
		socketPool.Put(s)
	}
	return errors.Merge(errs...)
}

func (s *socket) isActiveClosed() bool {
	return atomic.LoadInt32(&s.curState) == activeClose
}

func getProto(protoFuncs []ProtoFunc, rw io.ReadWriter) Proto {
	if len(protoFuncs) > 0 {
		return protoFuncs[0](rw)
	} else {
		return defaultProtoFunc(rw)
	}
}
