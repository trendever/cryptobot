package main

import (
	"common/rabbit"
	"core/proto"
)

var CreateOrder func(proto.Order) (proto.Order, error)
var CancelOrder func(orderID uint64) (bool, error)
var GetOrder func(orderID uint64) (proto.Order, error)

func init() {
	rabbit.DeclareRPC(proto.CreateOrder, &CreateOrder)
	rabbit.DeclareRPC(proto.CancelOrder, &CancelOrder)
	rabbit.DeclareRPC(proto.GetOrder, &GetOrder)
}
