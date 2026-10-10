package main

import "github.com/ehrlich-b/go-ublk"

// nullHandler measures transport overhead without accessing request buffers.
type nullHandler struct{}

func (nullHandler) NonBlocking() bool { return true }

func (nullHandler) HandleRequest(r *ublk.Request) {
	if err := r.Handle().Complete(nil); err != nil {
		panic(err)
	}
}

var _ ublk.NonBlockingDeclarer = nullHandler{}
