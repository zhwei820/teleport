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
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"sync"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/codec"
	"github.com/henrylee2cn/teleport/utils"
)

type (
	// Packet a socket data packet.
	Packet struct {
		// header object
		Header *Header
		// body content
		headerBytes []byte
		// header length
		HeaderLength int64
		// body codec type
		BodyType byte
		// body object
		Body interface{}
		// body content
		bodyBytes []byte
		// body length
		BodyLength int64
		// NewBody creates a new body by header info
		// Note:
		//  only for writing packet;
		//  should be nil when reading packet.
		NewBody NewBodyFunc
		// Contains transfer handlers from outer-most to inner-most.
		// Note: the length can not be bigger than 127!
		XferPipe []XferFilter
		next     *Packet
	}
	// Header header content of socket data packet.
	Header struct {
		Seq  uint64     // 8
		Type byte       // 1
		Uri  string     // len+4
		Code int32      // 4
		Meta utils.Args // len+4
	}
	// NewBodyFunc creates a new body by header info.
	NewBodyFunc func(*Header) interface{}
	// XferFilter handles byte stream of packet when transfer.
	XferFilter interface {
		Id() byte
		OnPack([]byte) ([]byte, error)
		OnUnpack([]byte) ([]byte, error)
	}
)

// MarshalBody returns the encoding of body.
func (p *Packet) MarshalBody() ([]byte, error) {
	if p.Body == nil {
		return []byte{}, nil
	}
	c, err := codec.GetById(p.BodyType)
	if err != nil {
		return err
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
	c, err := codec.GetById(p.BodyType)
	if err != nil {
		return err
	}
	p.Body = p.NewBody(p.Header)
	return c.Unmarshal(bodyBytes, p.Body)
}

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
func GetSenderPacket(typ int32, uri string, body interface{}, setting ...PacketSetting) *Packet {
	packet := GetPacket(nil, setting...)
	packet.Header.Type = typ
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
func NewSenderPacket(typ int32, uri string, body interface{}, setting ...PacketSetting) *Packet {
	packet := NewPacket(nil, setting...)
	packet.Header.Type = typ
	packet.Header.Uri = uri
	packet.Body = body
	return packet
}

// NewReceiverPacket returns a packet for sending.
func NewReceiverPacket(NewBody NewBodyFunc) *Packet {
	return NewPacket(NewBody)
}

// Reset resets itself.
// Note:
//  NewBody is only for reading form connection;
//  settings are only for writing to connection.
func (p *Packet) Reset(newBodyFunc NewBodyFunc, settings ...PacketSetting) {
	p.next = nil
	p.NewBody = newBodyFunc
	p.Header.Reset()
	p.Body = nil
	p.HeaderLength = 0
	p.BodyLength = 0
	p.Size = 0
	p.HeaderCodec = ""
	p.BodyType = ""
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

// BodyType returns packet body codec id.
func (p *Packet) BodyType() byte {
	c, err := codec.GetByName(p.BodyType)
	if err != nil {
		return codec.NilCodecId
	}
	return c.Id()
}

// PacketSetting sets Header field.
type PacketSetting func(*Packet)

// WithHeaderCodec sets header codec name.
func WithHeaderCodec(codecName string) PacketSetting {
	return func(p *Packet) {
		p.HeaderCodec = codecName
	}
}

// WithStatus sets header status.
func WithStatus(code int32, text string) PacketSetting {
	return func(p *Packet) {
		p.Header.StatusCode = code
		p.Header.Status = text
	}
}

// WithBodyType sets body codec name.
func WithBodyType(codecName string) PacketSetting {
	return func(p *Packet) {
		p.BodyType = codecName
	}
}

// GetCodecId returns codec id.
func GetCodecId(codecName string) byte {
	if len(codecName) == 0 {
		return codec.NilCodecId
	}
	c, err := codec.GetByName(codecName)
	if err != nil {
		return codec.NilCodecId
	}
	return c.Id()
}

// GetCodecName returns codec name.
func GetCodecName(codecId byte) string {
	if codecId == codec.NilCodecId {
		return ""
	}
	c, err := codec.GetById(codecId)
	if err != nil {
		return ""
	}
	return c.Name()
}

// GetCodecNameFromBytes returns codec name.
func GetCodecNameFromBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return GetCodecName(b[0])
}

// Unmarshal unmarshals bytes to header or body receiver.
func Unmarshal(b []byte, v interface{}, isGzip bool) (codecName string, err error) {
	switch recv := v.(type) {
	case nil:
		return "", nil

	case []byte:
		copy(recv, b)
		return "", nil

	case *[]byte:
		*recv = b
		return "", nil
	}

	var codecId byte
	limitReader := bytes.NewReader(b)
	err = binary.Read(limitReader, binary.BigEndian, &codecId)
	if err != nil {
		return "", err
	}

	c, err := codec.GetById(codecId)
	if err != nil {
		return "", err
	}

	var r io.Reader
	if isGzip {
		r, err = gzip.NewReader(limitReader)
		if err != nil {
			return GetCodecName(codecId), err
		}
		defer r.(*gzip.Reader).Close()
	} else {
		r = limitReader
	}

	return c.Name(), c.NewDecoder(r).Decode(v)
}

var (
	defaultBodyType codec.Codec
)

func init() {
	SetDefaultBodyType("json")
}

// GetDefaultBodyType gets the body default codec.
func GetDefaultBodyType() codec.Codec {
	return defaultBodyType
}

// SetDefaultBodyType set the default header codec.
// Note:
//  If the codec.Codec named 'codecName' is not registered, it will panic;
//  It is not safe to call it concurrently.
func SetDefaultBodyType(codecName string) {
	c, err := codec.GetByName(codecName)
	if err != nil {
		panic(err)
	}
	defaultBodyType = c
}

var (
	packetReadLimit    int64 = math.MaxInt64
	ErrExceedReadLimit       = errors.New("Size of package exceeds limit.")
)

// GetReadLimit gets the packet size upper limit of reading.
func GetReadLimit() int64 {
	return packetReadLimit
}

// GetPacketReadLimit sets max packet size.
// If maxSize<=0, set it to max int64.
func SetReadLimit(maxPacketSize int64) {
	if maxPacketSize <= 0 {
		packetReadLimit = math.MaxInt64
	} else {
		packetReadLimit = maxPacketSize
	}
}

func checkReadLimit(packetSize int64) error {
	if packetSize > packetReadLimit {
		return ErrExceedReadLimit
	}
	return nil
}
