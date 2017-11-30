package main

import (
	"common/rabbit"
	"core/proto"
	"lbapi"
)

var CheckKey func(lbapi.Key) (proto.Operator, error)
var OperatorByTg func(chatID int64) (proto.Operator, error)
var OperatorByID func(operatorID uint64) (proto.Operator, error)
var SetOperatorStatus func(proto.SetOperatorStatusRequest) (bool, error)
var SetOperatorKey func(proto.SetOperatorKeyRequest) (proto.Operator, error)
var AcceptOffer func(proto.AcceptOfferRequest) (proto.Order, error)
var SkipOffer func(proto.SkipOfferRequest) (bool, error)
var GetOrder func(id uint64) (proto.Order, error)
var DropOrder func(proto.DropOrderRequest) (bool, error)
var LinkLBContact func(proto.LinkLBContractRequest) (proto.Order, error)
var RequestPayment func(orderID uint64) (proto.Order, error)
var ConfirmPayment func(orderID uint64) (bool, error)

func init() {
	rabbit.DeclareRPC(proto.CheckKey, &CheckKey)
	rabbit.DeclareRPC(proto.OperatorByID, &OperatorByID)
	rabbit.DeclareRPC(proto.OperatorByTg, &OperatorByTg)
	rabbit.DeclareRPC(proto.SetOperatorStatus, &SetOperatorStatus)
	rabbit.DeclareRPC(proto.SetOperatorKey, &SetOperatorKey)
	rabbit.DeclareRPC(proto.AcceptOffer, &AcceptOffer)
	rabbit.DeclareRPC(proto.SkipOffer, &SkipOffer)
	rabbit.DeclareRPC(proto.GetOrder, &GetOrder)
	rabbit.DeclareRPC(proto.DropOrder, &DropOrder)
	rabbit.DeclareRPC(proto.LinkLBContact, &LinkLBContact)
	rabbit.DeclareRPC(proto.RequestPayment, &RequestPayment)
	rabbit.DeclareRPC(proto.ConfirmPayment, &ConfirmPayment)
}
