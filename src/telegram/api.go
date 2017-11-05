package main

import (
	"common/rabbit"
	"core/proto"
	"lbapi"
)

var CheckKey func(lbapi.Key) (proto.Operator, error)

func init() {
	rabbit.DeclareRPC(proto.CheckKey, &CheckKey)
}
