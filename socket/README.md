# Socket

A concise, powerful and high-performance TCP connection socket.

## Feature

- The server and client are peer-to-peer interfaces
- Support set the size of socket I/O buffer
- Support custom communication protocol
- Support custom transfer filter pipe (Such as gzip, encrypt, verify...)
- Packet contains both Header and Body
- Supports custom encoding types, e.g `JSON` `Protobuf`
- Header contains the status code and its description text
- Each socket is assigned an id
- Provides `Socket Hub`, `Socket` pool and `*Packet` stack
- Support setting the size of the reading packet (if exceed disconnect it)


## Benchmark

- Test server configuration

```
darwin amd64 4CPU 8GB
```

- teleport-socket

![tp_socket_benchmark](https://github.com/henrylee2cn/teleport/raw/develop/doc/tp_socket_benchmark.png)

**[test code](https://github.com/henrylee2cn/rpc-benchmark/tree/master/teleport)**

- rpcx

![rpcx_benchmark](https://github.com/henrylee2cn/teleport/raw/develop/doc/rpcx_benchmark.jpg)

**[test code](https://github.com/henrylee2cn/rpc-benchmark/tree/master/rpcx)**

## Keyworks

- **Packet:** The corresponding structure of the data package
- **Proto:** The protocol interface of packet pack/unpack 
- **Codec:** Serialization interface for `Packet.Body`
- **XferPipe:** A series of pipelines to handle packet data before transfer
- **XferFilter:** A interface to handle packet data before transfer


## Packet

The contents of every one packet:

```go
// socket package
type (
	// Packet a socket data packet.
	Packet struct {
		// packet size
		Size uint32 `json:"size"`
		// header object
		Header *Header `json:"header"`
		// body codec type
		BodyType byte `json:"body_type"`
		// body object
		Body interface{} `json:"body"`
		// NewBody creates a new body by header info
		// Note:
		//  only for writing packet;
		//  should be nil when reading packet.
		NewBody NewBodyFunc `json:"-"`
		// XferPipe transfer filter pipe, handlers from outer-most to inner-most.
		// Note: the length can not be bigger than 255!
		XferPipe xfer.XferPipe `json:"-"`
		next     *Packet
	}

	// Header header content of socket data packet.
	Header struct {
		Seq  uint64     `json:"seq"`
		Type byte       `json:"type"`
		Uri  string     `json:"uri"`
		Meta utils.Args `json:"-"`
	}

	// NewBodyFunc creates a new body by header info.
	NewBodyFunc func(*Header) interface{}
)

// xfer package
type (
	// XferPipe transfer filter pipe, handlers from outer-most to inner-most.
	// Note: the length can not be bigger than 255!
	XferPipe struct {
		filters []XferFilter
	}
	// XferFilter handles byte stream of packet when transfer.
	XferFilter interface {
		Id() byte
		OnPack([]byte) ([]byte, error)
		OnUnpack([]byte) ([]byte, error)
	}
)
```

## Protocol

You can customize your own communication protocol by implementing the interface:

```go
type (
	// Proto pack/unpack protocol scheme of socket packet.
	Proto interface {
		// Version returns the protocol's id and name.
		Version() (byte, string)
		// Pack pack socket data packet.
		// Note: Make sure to write only once or there will be package contamination!
		Pack(*Packet) error
		// Unpack unpack socket data packet.
		// Note: Concurrent unsafe!
		Unpack(*Packet) error
	}
	ProtoFunc func(io.ReadWriter) Proto
)
```

Next, you can specify the communication protocol in the following ways:

```go
func SetDefaultProtoFunc(ProtoFunc)
func GetSocket(net.Conn, ...ProtoFunc) Socket
func NewSocket(net.Conn, ...ProtoFunc) Socket
```

## Demo

### server.go

```go
package main

import (
	"log"
	"net"

	"github.com/henrylee2cn/teleport/socket"
	"github.com/henrylee2cn/teleport/socket/example/pb"
)

func main() {
	// socket.SetPacketSizeLimit(512)
	lis, err := net.Listen("tcp", "0.0.0.0:8000")
	if err != nil {
		log.Fatalf("[SVR] listen err: %v", err)
	}
	log.Printf("listen tcp 0.0.0.0:8000")
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Fatalf("[SVR] accept err: %v", err)
		}
		go func(s socket.Socket) {
			log.Printf("accept %s", s.Id())
			defer s.Close()
			var pbTest = new(pb.PbTest)
			for {
				// read request
				var packet = socket.GetPacket(func(_ *socket.Header) interface{} {
					*pbTest = pb.PbTest{}
					return pbTest
				})
				err = s.ReadPacket(packet)
				if err != nil {
					log.Printf("[SVR] read request err: %v", err)
					return
				} else {
					log.Printf("[SVR] read request: %v", packet)
				}

				// write response
				pbTest.A = pbTest.A + pbTest.B
				pbTest.B = pbTest.A - pbTest.B*2
				packet.Body = pbTest

				err = s.WritePacket(packet)
				if err != nil {
					log.Printf("[SVR] write response err: %v", err)
				} else {
					log.Printf("[SVR] write response: %v", packet)
				}
				socket.PutPacket(packet)
			}
		}(socket.GetSocket(conn))
	}
}
```

### client.go

```go
package main

import (
	"log"
	"net"

	"github.com/henrylee2cn/teleport/codec"
	"github.com/henrylee2cn/teleport/socket"

	"github.com/henrylee2cn/teleport/socket/example/pb"
)

func main() {
	conn, err := net.Dial("tcp", "127.0.0.1:8000")
	if err != nil {
		log.Fatalf("[CLI] dial err: %v", err)
	}
	s := socket.GetSocket(conn)
	defer s.Close()
	var packet = socket.GetPacket(nil)
	defer socket.PutPacket(packet)
	for i := uint64(0); i < 1; i++ {
		// write request
		packet.Reset(nil)
		packet.Header.Type = 0
		packet.BodyType = codec.ID_JSON
		packet.Header.Seq = i
		packet.Header.Uri = "/a/b"
		packet.Body = &pb.PbTest{A: 10, B: 2}
		err = s.WritePacket(packet)
		if err != nil {
			log.Printf("[CLI] write request err: %v", err)
			continue
		}
		log.Printf("[CLI] write request: %v", packet)

		// read response
		packet.Reset(func(_ *socket.Header) interface{} {
			return new(pb.PbTest)
		})
		err = s.ReadPacket(packet)
		if err != nil {
			log.Printf("[CLI] read response err: %v", err)
		} else {
			log.Printf("[CLI] read response: %v", packet)
		}
	}
	// select {}
}
```