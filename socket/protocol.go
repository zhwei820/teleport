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
	"encoding/binary"
	"errors"
	"io"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/utils"
)

type (
	// Proto pack/unpack protocol scheme of socket packet.
	Proto interface {
		// Version returns the protocol's id and name.
		Version() (byte, string)
		// Pack pack socket data packet.
		Pack(io.Writer, *Packet) error
		// Unpack unpack socket data packet.
		Unpack(io.Reader, *Packet) error
	}
	ProtoFunc func() Proto
)

// DefaultProtoFunc gets the default builder of socket communication protocol
func DefaultProtoFunc() ProtoFunc {
	return defaultProtoFunc
}

// SetDefaultProtoFunc sets the default builder of socket communication protocol
func SetDefaultProtoFunc(protocolFunc ProtoFunc) {
	defaultProtoFunc = protocolFunc
}

/*
```
	HeaderLength | HeaderCodecId | Header | BodyLength | BodyTypeId | Body
	```

	**Notes:**

	- `HeaderLength`: uint32, 4 bytes, big endian
	- `HeaderCodecId`: uint8, 1 byte
	- `Header`: header bytes
	- `BodyLength`: uint32, 4 bytes, big endian
		* may be 0, meaning that the `Body` is empty and does not indicate the `BodyTypeId`
		* may be 1, meaning that the `Body` is empty but indicates the `BodyTypeId`
	- `BodyTypeId`: uint8, 1 byte
	- `Body`: body bytes
*/

type (
	// FastProto fast socket communication protocol.
	FastProto struct {
		id   byte
		name string
	}
)

// default builder of socket communication protocol
var (
	defaultProtoFunc = func() Proto { return &FastProto{'f', "fast"} }
	lengthSize       = int64(binary.Size(uint32(0)))
)

// error
var (
	ErrProtoUnmatch = errors.New("Mismatched protocol")
)

// Version returns the protocol's id and name.
func (f *FastProto) Version() (byte, string) {
	return f.id, f.name
}

// Pack pack socket data packet.
func (f *FastProto) Pack(w io.Writer, p *Packet) error {
	bb1 := utils.AcquireByteBuffer()
	defer utils.ReleaseByteBuffer(bb1)

	// protocol version
	bb1.WriteByte(f.id)

	// transfer pipe
	bb1.WriteByte(byte(p.XferPipe.Len()))
	bb1.Write(p.XferPipe.Ids())

	bb2 := utils.AcquireByteBuffer()
	defer utils.ReleaseByteBuffer(bb2)

	// header
	err := f.writeHeader(bb2, p)
	if err != nil {
		return err
	}

	// body
	err = f.writeBody(bb2, p)
	if err != nil {
		return err
	}

	// do transfer pipe
	bb2.B, err = p.XferPipe.OnPack(bb2.B)
	if err != nil {
		return err
	}

	// real write
	err = p.SetSizeAndCheck(uint32(4 + bb1.Len() + bb2.Len()))
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.BigEndian, p.GetSize())
	if err != nil {
		return err
	}
	_, err = w.Write(bb1.Bytes())
	if err != nil {
		return err
	}
	_, err = w.Write(bb2.Bytes())
	return err
}

func (f *FastProto) writeHeader(bb *utils.ByteBuffer, p *Packet) error {
	binary.Write(bb, binary.BigEndian, p.Header.Seq)

	bb.WriteByte(p.Header.Type)

	uriBytes := goutil.StringToBytes(p.Header.Uri)
	binary.Write(bb, binary.BigEndian, uint32(len(uriBytes)))
	bb.Write(uriBytes)

	binary.Write(bb, binary.BigEndian, p.Header.Code)

	metaBytes := p.Header.Meta.QueryString()
	binary.Write(bb, binary.BigEndian, uint32(len(metaBytes)))
	bb.Write(metaBytes)
	return nil
}

func (f *FastProto) writeBody(bb *utils.ByteBuffer, p *Packet) error {
	bb.WriteByte(p.BodyType)
	bodyBytes, err := p.MarshalBody()
	if err != nil {
		return err
	}
	bb.Write(bodyBytes)
	return nil
}

// Unpack unpack socket data packet.
func (f *FastProto) Unpack(r io.Reader, p *Packet) (err error) {
	// size
	var size uint32
	err = binary.Read(r, binary.BigEndian, &size)
	if err != nil {
		return err
	}
	if err = p.SetSizeAndCheck(size); err != nil {
		return err
	}
	// protocol
	one := make([]byte, 1)
	_, err = r.Read(one)
	if err != nil {
		return err
	}
	if one[0] != f.id {
		return ErrProtoUnmatch
	}
	// transfer pipe
	_, err = r.Read(one)
	if err != nil {
		return err
	}
	var xferLen = one[0]
	for i := xferLen; i > 0; i-- {
		_, err = r.Read(one)
		if err != nil {
			return err
		}
		err = p.XferPipe.Append(one[0])
		if err != nil {
			return err
		}
	}
	// read last all
	var lastLen = size - 4 - 1 - 1 - uint32(xferLen)
	data := make([]byte, lastLen)
	if _, err = io.ReadFull(r, data); err != nil {
		return err
	}
	// do transfer pipe
	data, err = p.XferPipe.OnUnpack(data)
	if err != nil {
		return err
	}
	// header
	err = f.readHeader(&data, p)
	if err == nil {
		// body
		err = f.readBody(data, p)
	}
	return err
}

func (f *FastProto) readHeader(dataPtr *[]byte, p *Packet) error {
	data := *dataPtr
	// seq
	p.Header.Seq = binary.BigEndian.Uint64(data)
	data = data[8:]
	// type
	p.Header.Type = data[0]
	data = data[1:]
	// uri
	uriLen := binary.BigEndian.Uint32(data)
	data = data[4:]
	p.Header.Uri = string(data[:uriLen])
	data = data[uriLen:]
	// code
	p.Header.Code = int32(binary.BigEndian.Uint32(data))
	data = data[4:]
	// meta
	metaLen := binary.BigEndian.Uint32(data)
	data = data[4:]
	p.Header.Meta.ParseBytes(data[:metaLen])
	data = data[metaLen:]
	*dataPtr = data
	return nil
}

func (f *FastProto) readBody(data []byte, p *Packet) error {
	p.BodyType = data[0]
	return p.UnmarshalBody(data[1:])
}
