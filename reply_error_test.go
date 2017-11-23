package teleport

import (
	"testing"

	"github.com/henrylee2cn/teleport/socket"
)

func TestReError(t *testing.T) {
	reErr := new(ReError)
	t.Logf("%v", reErr)
	reErr.Code = 400
	reErr.Message = "msg"
	t.Logf("%v", reErr)
	reErr.Detail = `"bala...bala..."`
	t.Logf("%v", reErr)
	header := &socket.Header{}
	reErr.SetToMeta(header)
	t.Logf("%v", header.Meta.String())
	b := header.Meta.Peek(ReErrorMeta)
	t.Logf("%s", b)
	newReErr := new(ReError)
	newReErr.SetFromMeta(header)
	t.Logf("%v", newReErr)
}
