package main

import (
	"time"

	tp "github.com/henrylee2cn/teleport"
)

func main() {
	go tp.GraceSignal()
	tp.SetShutdown(time.Second*20, nil, nil)
	for jj:=1;jj<10000;jj++ {

		var peer = tp.NewPeer(tp.PeerConfig{
			SlowCometDuration: time.Millisecond * 500,
		})
		defer peer.Close()
		peer.RoutePush(new(Push))

		var sess, rerr= peer.Dial("127.0.0.1:9090")
		if rerr != nil {
			tp.Fatalf("%v", rerr)
		}
		var reply []byte
		go func() {
			for ii := 1; ii < 1000; ii++ {
				if rerr = sess.Pull(
					"/group/home/test?peer_id=call-1",
					[]byte("pull text"),
					&reply,
					tp.WithQuery("a", "1"),
				).Rerror(); rerr != nil {
					tp.Errorf("pull error: %v", rerr)
					time.Sleep(time.Second * 2)
				} else {
					//break
				}
			}
		}()

		tp.Infof("test reply: %s", reply)
	}
	//
	//rerr = sess.Pull(
	//	"/group/home/test_unknown?peer_id=call-2",
	//	[]byte("unknown pull text"),
	//	&reply,
	//	tp.WithQuery("b", "2"),
	//).Rerror()
	//if tp.IsConnRerror(rerr) {
	//	tp.Fatalf("has conn rerror: %v", rerr)
	//}
	//if rerr != nil {
	//	tp.Fatalf("pull error: %v", rerr)
	//}
	//tp.Infof("test_unknown: %s", reply)
	select {

	}
}

// Push controller
type Push struct {
	tp.PushCtx
}

// Test handler
func (p *Push) Test(args *[]byte) *tp.Rerror {
	//tp.Infof("receive push(%s):\nargs: %s\nquery: %#v\n", p.Ip(), *args, p.Query())
	return nil
}
