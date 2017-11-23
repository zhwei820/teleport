package teleport

import (
	"testing"

	"github.com/henrylee2cn/teleport/utils"
)

func TestRerror(t *testing.T) {
	reErr := new(Rerror)
	t.Logf("%v", reErr)
	reErr.Code = 400
	reErr.Message = "msg"
	t.Logf("%v", reErr)
	reErr.Detail = `"bala...bala..."`
	t.Logf("%v", reErr)
	meta := &utils.Args{}
	reErr.SetToMeta(meta)
	t.Logf("%v", meta.String())
	b := meta.Peek(MetaRerrorKey)
	t.Logf("%s", b)
	newReErr := NewRerrorFromMeta(meta)
	t.Logf("%v", newReErr)
}
