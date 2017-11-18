package main

import (
	"common/rabbit"
	"core/proto"
	"lbapi"
)

var CheckKey func(lbapi.Key) (proto.Operator, error)
var OperatorByTd func(chatID int64) (proto.Operator, error)
var SetOperatorStatus func(proto.SetOperatorStatusRequest) (bool, error)
var SetOperatorKey func(proto.SetOperatorKeyRequest) (proto.Operator, error)

func init() {
	rabbit.DeclareRPC(proto.CheckKey, &CheckKey)
	rabbit.DeclareRPC(proto.OperatorByTg, &OperatorByTd)
	rabbit.DeclareRPC(proto.SetOperatorStatus, &SetOperatorStatus)
	rabbit.DeclareRPC(proto.SetOperatorKey, &SetOperatorKey)
}
