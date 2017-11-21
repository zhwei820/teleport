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
	"io"
	"io/ioutil"
	"math"
	"sync"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/codec"
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
		Unpack(*Packet, io.Reader) error
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
	HeaderLength | HeaderCodecId | Header | BodyLength | BodyCodecId | Body
	```

	**Notes:**

	- `HeaderLength`: uint32, 4 bytes, big endian
	- `HeaderCodecId`: uint8, 1 byte
	- `Header`: header bytes
	- `BodyLength`: uint32, 4 bytes, big endian
		* may be 0, meaning that the `Body` is empty and does not indicate the `BodyCodecId`
		* may be 1, meaning that the `Body` is empty but indicates the `BodyCodecId`
	- `BodyCodecId`: uint8, 1 byte
	- `Body`: body bytes
*/

// default builder of socket communication protocol
var (
	defaultProtoFunc = func() Proto { return &FastProto{tmpBufferWriter: bytes.NewBuffer(nil)} }
	lengthSize       = int64(binary.Size(uint32(0)))
)

type (
	// FastProto fast socket communication protocol.
	FastProto struct {
		tmpBufferWriter *bytes.Buffer
	}
)

// Version returns the protocol's id and name.
func (f *FastProto) Version() (byte, string) {
	return 'f', "fast"
}

var (
	ErrXferPipeTooLong = errors.New("the length of transfer pipe cannot be bigger than 127")
)

// Pack pack socket data packet.
func (f *FastProto) Pack(w io.Writer, p *Packet) error {
	bb1 := utils.AcquireByteBuffer()
	defer utils.ReleaseByteBuffer(bb1)

	// protocol version
	bb1.WriteByte('f')

	// transfer pipe
	if len(p.XferPipe) > math.MaxInt8 {
		return ErrXferPipeTooLong
	}

	bb1.WriteByte(byte(len(p.XferPipe)))
	for _, pipe := range p.XferPipe {
		bb1.WriteByte(pipe.Id())
	}

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
	for _, pipe := range p.XferPipe {
		if bb2.B, err = pipe.OnPack(bb2.B); err != nil {
			return err
		}
	}

	// real write
	err = binary.Write(w, binary.BigEndian, uint32(bb1.Len()+bb2.Len()))
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
}

func (f *FastProto) writeBody(bb *utils.ByteBuffer, p *Packet) error {
	bodyBytes, err := p.MarshalBody()
	if err != nil {
		return err
	}
	binary.Write(bb, binary.BigEndian, uint32(len(bodyBytes)))
	bb.Write(bodyBytes)
	return nil
}

// Unpack unpack socket data packet.
func (f *FastProto) Unpack(p *Packet, r io.Reader) error {

}

// WritePacket writes header and body to the connection.
// WritePacket can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetWriteDeadline.
// Note:
//  For the byte stream type of body, write directly, do not do any processing;
//  Must be safe for concurrent use by multiple goroutines.
func (p *FastProto) WritePacket(
	packet *Packet,
	destWriter *utils.BufioWriter,
	codecWriterMaker func(codecName string, w io.Writer) (*CodecWriter, error),
	isActiveClosed func() bool,
) error {

	// write header
	p.tmpBufferWriter.Reset()
	codecWriter, err := codecWriterMaker(packet.HeaderCodec, p.tmpBufferWriter)
	if err != nil {
		return err
	}
	err = p.writeHeader(destWriter, codecWriter, packet.Header)
	packet.Size = destWriter.Count()
	packet.HeaderLength = destWriter.Count() - lengthSize
	packet.BodyLength = 0
	if err != nil {
		return err
	}

	// write body
	switch bo := packet.Body.(type) {
	case nil:
		err = binary.Write(destWriter, binary.BigEndian, uint32(1))
		if err == nil {
			err = destWriter.WriteByte(GetCodecId(packet.BodyCodec))
		}

	case []byte:
		err = p.writeBytesBody(destWriter, bo)
	case *[]byte:
		err = p.writeBytesBody(destWriter, *bo)
	default:
		p.tmpBufferWriter.Reset()
		codecWriter, err = codecWriterMaker(packet.BodyCodec, p.tmpBufferWriter)
		if err == nil {
			err = p.writeBody(destWriter, codecWriter, int(packet.Header.Gzip), bo)
		}
	}
	if err == nil {
		err = destWriter.Flush()
	}
	packet.Size = destWriter.Count()
	packet.BodyLength = packet.Size - packet.HeaderLength - lengthSize*2
	return err
}

func (p *FastProto) writeHeader(destWriter *utils.BufioWriter, codecWriter *CodecWriter, header *Header) error {
	err := p.tmpBufferWriter.WriteByte(codecWriter.Id())
	if err != nil {
		return err
	}
	err = codecWriter.Encode(gzip.NoCompression, header)
	if err != nil {
		return err
	}
	headerLength := uint32(p.tmpBufferWriter.Len())
	err = binary.Write(destWriter, binary.BigEndian, headerLength)
	if err != nil {
		return err
	}
	_, err = p.tmpBufferWriter.WriteTo(destWriter)
	return err
}

func (FastProto) writeBytesBody(destWriter *utils.BufioWriter, body []byte) error {
	bodyLength := uint32(len(body))
	err := binary.Write(destWriter, binary.BigEndian, bodyLength)
	if err != nil {
		return err
	}
	_, err = destWriter.Write(body)
	return err
}

func (p *FastProto) writeBody(destWriter *utils.BufioWriter, codecWriter *CodecWriter, gzipLevel int, body interface{}) error {
	err := p.tmpBufferWriter.WriteByte(codecWriter.Id())
	if err != nil {
		return err
	}
	err = codecWriter.Encode(gzipLevel, body)
	if err != nil {
		return err
	}
	// write body to socket buffer
	bodyLength := uint32(p.tmpBufferWriter.Len())
	err = binary.Write(destWriter, binary.BigEndian, bodyLength)
	if err != nil {
		return err
	}
	_, err = p.tmpBufferWriter.WriteTo(destWriter)
	return err
}

// ReadPacket reads header and body from the connection.
// Note:
//  For the byte stream type of body, read directly, do not do any processing;
//  Must be safe for concurrent use by multiple goroutines.
func (p FastProto) ReadPacket(
	packet *Packet,
	bodyAdapter func() interface{},
	srcReader *utils.BufioReader,
	codecReaderMaker func(codecId byte) (*CodecReader, error),
	isActiveClosed func() bool,
	checkReadLimit func(int64) error,
) error {

	var (
		hErr, bErr error
		b          interface{}
	)
	srcReader.ResetCount()
	packet.HeaderCodec, hErr = p.readHeader(srcReader, codecReaderMaker, packet.Header, checkReadLimit)
	packet.Size = srcReader.Count()
	if srcReader.Count() > lengthSize {
		packet.HeaderLength = srcReader.Count() - lengthSize
	}

	if hErr == nil {
		b = bodyAdapter()
	} else {
		if hErr == io.EOF || hErr == io.ErrUnexpectedEOF {
			packet.Size = packet.HeaderLength
			packet.BodyLength = 0
			packet.BodyCodec = ""
			return hErr
		} else if isActiveClosed() {
			packet.Size = packet.HeaderLength
			packet.BodyLength = 0
			packet.BodyCodec = ""
			return ErrProactivelyCloseSocket
		}
	}

	srcReader.ResetCount()
	packet.BodyCodec, bErr = p.readBody(srcReader, codecReaderMaker, int(packet.Header.Gzip), b, packet.HeaderLength, checkReadLimit)
	packet.Size += srcReader.Count()
	if srcReader.Count() > lengthSize {
		packet.BodyLength = srcReader.Count() - lengthSize
	}
	if isActiveClosed() {
		return ErrProactivelyCloseSocket
	}
	return bErr
}

// readHeader reads header from the connection.
// readHeader can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetReadDeadline.
// Note: must use only one goroutine call.
func (FastProto) readHeader(
	srcReader *utils.BufioReader,
	codecReaderMaker func(byte) (*CodecReader, error),
	header *Header,
	checkReadLimit func(int64) error,
) (string, error) {

	srcReader.ResetLimit(-1)

	var headerLength uint32
	err := binary.Read(srcReader, binary.BigEndian, &headerLength)
	if err != nil {
		return "", err
	}

	// check packet size
	err = checkReadLimit(int64(headerLength) + lengthSize)
	if err != nil {
		return "", err
	}

	srcReader.ResetLimit(int64(headerLength))

	codecId, err := srcReader.ReadByte()
	if err != nil {
		return GetCodecName(codecId), err
	}

	codecReader, err := codecReaderMaker(codecId)
	if err != nil {
		return GetCodecName(codecId), err
	}

	err = codecReader.Decode(gzip.NoCompression, header)
	return codecReader.Name(), err
}

// readBody reads body from the connection.
// readBody can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetReadDeadline.
// Note: must use only one goroutine call, and it must be called after calling the readHeader().
func (FastProto) readBody(
	srcReader *utils.BufioReader,
	codecReaderMaker func(byte) (*CodecReader, error),
	gzipLevel int,
	body interface{},
	headerLength int64,
	checkReadLimit func(int64) error,
) (string, error) {

	srcReader.ResetLimit(-1)

	var (
		bodyLength uint32
		codecId    = codec.NilCodecId
	)

	err := binary.Read(srcReader, binary.BigEndian, &bodyLength)
	if err != nil {
		return "", err
	}
	if bodyLength == 0 {
		return "", err
	}

	// check packet size
	err = checkReadLimit(headerLength + int64(bodyLength) + lengthSize*2)
	if err != nil {
		return "", err
	}

	srcReader.ResetLimit(int64(bodyLength))

	// read body
	switch bo := body.(type) {
	case nil:
		var codecName string
		codecName, err = readAll(srcReader, make([]byte, bodyLength))
		return codecName, err

	case []byte:
		var codecName string
		codecName, err = readAll(srcReader, bo)
		return codecName, err

	case *[]byte:
		*bo, err = ioutil.ReadAll(srcReader)
		return GetCodecNameFromBytes(*bo), err

	default:
		codecId, err = srcReader.ReadByte()
		if bodyLength == 1 || err != nil {
			return GetCodecName(codecId), err
		}
		codecReader, err := codecReaderMaker(codecId)
		if err != nil {
			return GetCodecName(codecId), err
		}
		err = codecReader.Decode(gzipLevel, body)
		return codecReader.Name(), err
	}
}

func readAll(reader io.Reader, p []byte) (string, error) {
	perLen := len(p)
	_, err := reader.Read(p[:perLen])
	if err == nil {
		_, err = io.Copy(ioutil.Discard, reader)
	}
	return GetCodecNameFromBytes(p), err
}
