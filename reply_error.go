package teleport

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/socket"
	"github.com/tidwall/gjson"
)

type (
	// ReError error only for reply packet
	ReError struct {
		// Code error code
		Code int32
		// Message error message to the user
		Message string
		// Detail error's detailed reason
		Detail string
	}
)

const ReErrorMeta = "X-Reply-Error"

var (
	_ json.Marshaler   = new(ReError)
	_ json.Unmarshaler = new(ReError)
	_ error            = new(ReError)

	re_a = []byte(`{"code":`)
	re_b = []byte(`,"message":"`)
	re_c = []byte(`,"detail":"`)
	re_d = []byte(`"`)
	re_e = []byte(`\"`)
)

// Error implements error interface
func (r *ReError) Error() string {
	b, _ := r.MarshalJSON()
	return goutil.BytesToString(b)
}

// SetToMeta sets self to header 'X-Reply-Error' metadata.
func (r *ReError) SetToMeta(header *socket.Header) {
	errStr := r.Error()
	if len(errStr) == 0 {
		return
	}
	header.Meta.Set(ReErrorMeta, errStr)
}

// SetFromMeta sets self from header 'X-Reply-Error' metadata.
func (r *ReError) SetFromMeta(header *socket.Header) {
	if r == nil {
		return
	}
	b := header.Meta.Peek(ReErrorMeta)
	r.UnmarshalJSON(b)
}

// MarshalJSON marshals ReError into JSON, implements json.Marshaler interface.
func (r *ReError) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte{}, nil
	}
	var b = append(re_a, strconv.FormatInt(int64(r.Code), 10)...)
	if len(r.Message) > 0 {
		b = append(b, re_b...)
		b = append(b, bytes.Replace(goutil.StringToBytes(r.Message), re_d, re_e, -1)...)
		b = append(b, '"')
	}
	if len(r.Detail) > 0 {
		b = append(b, re_c...)
		b = append(b, bytes.Replace(goutil.StringToBytes(r.Detail), re_d, re_e, -1)...)
		b = append(b, '"')
	}
	b = append(b, '}')
	return b, nil
}

// UnmarshalJSON unmarshals a JSON description of self.
func (r *ReError) UnmarshalJSON(b []byte) error {
	if r == nil {
		return nil
	}
	s := goutil.BytesToString(b)
	r.Code = int32(gjson.Get(s, "code").Int())
	r.Message = gjson.Get(s, "message").String()
	r.Detail = gjson.Get(s, "detail").String()
	return nil
}
