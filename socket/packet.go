// Socket package provides a concise, powerful and high-performance TCP
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

package socket

import (
	"encoding/json"
	"errors"
	"math"
	"sync"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/codec"
	"github.com/henrylee2cn/teleport/utils"
	"github.com/henrylee2cn/teleport/xfer"
)

var packetStack = new(struct {
	freePacket *Packet
	mu         sync.Mutex
})

// GetPacket gets a *Packet form packet stack.
// Note:
//  NewBody is only for reading form connection;
//  settings are only for writing to connection.
func GetPacket(NewBody NewBodyFunc, settings ...PacketSetting) *Packet {
	packetStack.mu.Lock()
	p := packetStack.freePacket
	if p == nil {
		p = NewPacket(NewBody)
	} else {
		packetStack.freePacket = p.next
		p.Reset(NewBody, settings...)
	}
	packetStack.mu.Unlock()
	return p
}

// GetSenderPacket returns a packet for sending.
func GetSenderPacket(packetType byte, uri string, body interface{}, setting ...PacketSetting) *Packet {
	packet := GetPacket(nil, setting...)
	packet.Header.Type = packetType
	packet.Header.Uri = uri
	packet.Body = body
	return packet
}

// GetReceiverPacket returns a packet for sending.
func GetReceiverPacket(NewBody NewBodyFunc) *Packet {
	return GetPacket(NewBody)
}

// PutPacket puts a *Packet to packet stack.
func PutPacket(p *Packet) {
	packetStack.mu.Lock()
	p.Body = nil
	p.next = packetStack.freePacket
	packetStack.freePacket = p
	packetStack.mu.Unlock()
}

// NewPacket creates a new *Packet.
// Note:
//  NewBody is only for reading form connection;
//  settings are only for writing to connection.
func NewPacket(NewBody NewBodyFunc, settings ...PacketSetting) *Packet {
	var p = &Packet{
		Header:  new(Header),
		NewBody: NewBody,
	}
	for _, f := range settings {
		f(p)
	}
	return p
}

// NewSenderPacket returns a packet for sending.
func NewSenderPacket(packetType byte, uri string, body interface{}, setting ...PacketSetting) *Packet {
	packet := NewPacket(nil, setting...)
	packet.Header.Type = packetType
	packet.Header.Uri = uri
	packet.Body = body
	return packet
}

// NewReceiverPacket returns a packet for sending.
func NewReceiverPacket(NewBody NewBodyFunc) *Packet {
	return NewPacket(NewBody)
}

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
		Seq  uint64
		Type byte
		Uri  string
		Meta utils.Args
	}

	// NewBodyFunc creates a new body by header info.
	NewBodyFunc func(*Header) interface{}
)

// SetSizeAndCheck sets the size of packet.
// If the size is too big, returns error.
func (p *Packet) SetSizeAndCheck(size uint32) error {
	err := checkPacketSize(size)
	if err != nil {
		return err
	}
	p.Size = size
	return nil
}

// GetSize returns the size of packet.
func (p *Packet) GetSize() uint32 {
	return p.Size
}

// MarshalBody returns the encoding of body.
func (p *Packet) MarshalBody() ([]byte, error) {
	if p.Body == nil {
		return []byte{}, nil
	}
	c, err := codec.Get(p.BodyType)
	if err != nil {
		return []byte{}, err
	}
	return c.Marshal(p.Body)
}

// UnmarshalBody parses the encoded data and stores the result
// in the value pointed to by v.
func (p *Packet) UnmarshalBody(bodyBytes []byte) error {
	if len(bodyBytes) == 0 {
		return nil
	}
	if p.NewBody == nil {
		p.Body = nil
		return nil
	}
	c, err := codec.Get(p.BodyType)
	if err != nil {
		return err
	}
	p.Body = p.NewBody(p.Header)
	switch body := p.Body.(type) {
	default:
		return c.Unmarshal(bodyBytes, p.Body)
	case *[]byte:
		if body != nil {
			*body = bodyBytes
		}
		return nil
	}
}

// Reset resets itself.
// Note:
//  NewBody is only for reading form connection;
//  settings are only for writing to connection.
func (p *Packet) Reset(newBodyFunc NewBodyFunc, settings ...PacketSetting) {
	p.next = nil
	p.NewBody = newBodyFunc
	*p.Header = Header{}
	p.Body = nil
	p.Size = 0
	p.BodyType = codec.NilCodecId
	p.XferPipe.Reset()
	for _, f := range settings {
		f(p)
	}
}

// SetNewBody resets the function of geting body.
func (p *Packet) SetNewBody(newBodyFunc NewBodyFunc) {
	p.NewBody = newBodyFunc
}

// String returns printing text.
func (p *Packet) String() string {
	b, _ := json.MarshalIndent(p, "", "  ")
	return goutil.BytesToString(b)
}

// PacketSetting sets Header field.
type PacketSetting func(*Packet)

// WithBodyType sets body codec name.
func WithBodyType(codecId byte) PacketSetting {
	return func(p *Packet) {
		p.BodyType = codecId
	}
}

// WithXferPipe sets transfer filter pipe.
func WithXferPipe(filterId ...byte) PacketSetting {
	return func(p *Packet) {
		p.XferPipe.Append(filterId...)
	}
}

var (
	defaultBodyType codec.Codec
)

func init() {
	SetDefaultBodyType(codec.ID_JSON)
}

// GetDefaultBodyType gets the body default codec.
func GetDefaultBodyType() codec.Codec {
	return defaultBodyType
}

// SetDefaultBodyType set the default header codec.
// Note:
//  If the codec.Codec named 'codecId' is not registered, it will panic;
//  It is not safe to call it concurrently.
func SetDefaultBodyType(codecId byte) {
	c, err := codec.Get(codecId)
	if err != nil {
		panic(err)
	}
	defaultBodyType = c
}

var (
	packetSizeLimit uint32 = math.MaxUint32
	// ErrExceedPacketSizeLimit error
	ErrExceedPacketSizeLimit = errors.New("Size of package exceeds limit.")
)

// PacketSizeLimit gets the packet size upper limit of reading.
func PacketSizeLimit() uint32 {
	return packetSizeLimit
}

// SetPacketSizeLimit sets max packet size.
// If maxSize<=0, set it to max uint32.
func SetPacketSizeLimit(maxPacketSize uint32) {
	if maxPacketSize <= 0 {
		packetSizeLimit = math.MaxUint32
	} else {
		packetSizeLimit = maxPacketSize
	}
}

func checkPacketSize(packetSize uint32) error {
	if packetSize > packetSizeLimit {
		return ErrExceedPacketSizeLimit
	}
	return nil
}
