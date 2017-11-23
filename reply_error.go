package teleport

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/henrylee2cn/goutil"
	"github.com/henrylee2cn/teleport/utils"
	"github.com/tidwall/gjson"
)

type (
	// Rerror error only for reply packet
	Rerror struct {
		// Code error code
		Code int32
		// Message error message to the user
		Message string
		// Detail error's detailed reason
		Detail string
	}
)

const MetaRerrorKey = "X-Reply-Error"

var (
	_ json.Marshaler   = new(Rerror)
	_ json.Unmarshaler = new(Rerror)
	_ error            = new(Rerror)

	re_a = []byte(`{"code":`)
	re_b = []byte(`,"message":"`)
	re_c = []byte(`,"detail":"`)
	re_d = []byte(`"`)
	re_e = []byte(`\"`)
)

// Error implements error interface
func (r *Rerror) Error() string {
	b, _ := r.MarshalJSON()
	return goutil.BytesToString(b)
}

// SetToMeta sets self to header 'X-Reply-Error' metadata.
func (r *Rerror) SetToMeta(meta *utils.Args) {
	errStr := r.Error()
	if len(errStr) == 0 {
		return
	}
	meta.Set(MetaRerrorKey, errStr)
}

// NewRerrorFromMeta creates a *Rerror from header 'X-Reply-Error' metadata.
// Return nil if there is no 'X-Reply-Error' in header metadata.
func NewRerrorFromMeta(meta *utils.Args) *Rerror {
	if meta == nil {
		return nil
	}
	b := meta.Peek(MetaRerrorKey)
	if len(b) == 0 {
		return nil
	}
	r := new(Rerror)
	r.UnmarshalJSON(b)
	return r
}

// MarshalJSON marshals Rerror into JSON, implements json.Marshaler interface.
func (r *Rerror) MarshalJSON() ([]byte, error) {
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
func (r *Rerror) UnmarshalJSON(b []byte) error {
	if r == nil {
		return nil
	}
	s := goutil.BytesToString(b)
	r.Code = int32(gjson.Get(s, "code").Int())
	r.Message = gjson.Get(s, "message").String()
	r.Detail = gjson.Get(s, "detail").String()
	return nil
}
