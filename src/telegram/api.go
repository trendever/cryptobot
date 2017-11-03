package main

import (
	"common/rabbit"
	"localbitcoins/lbapi"
	"localbitcoins/proto"
)

var CheckKey func(lbapi.Key) (proto.Operator, error)

func init() {
	rabbit.DeclareRPC(proto.CheckKey, &CheckKey)
}
