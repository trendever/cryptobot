package proto

import (
	"common/rabbit"
	"lbapi"
)

type OperatorStatus int

const (
	// Account do not have valid keypair.
	OperatorStatus_None OperatorStatus = 0
	// Account is valid but unready to accept offers.
	OperatorStatus_Inactive OperatorStatus = 1
	// Account is ready to accept offers.
	OperatorStatus_Ready OperatorStatus = 2
	// In action.
	OperatorStatus_Busy OperatorStatus = 3
)

type Operator struct {
	ID           uint64
	Username     string
	TelegramChat int64
	Status       OperatorStatus
}

var CheckKey = rabbit.RPC{
	Name:        "check_lb_key",
	HandlerType: (func(lbapi.Key) (Operator, error))(nil),
}

var OperatorByTg = rabbit.RPC{
	Name:        "operator_by_tg",
	HandlerType: (func(chatID int64) (Operator, error))(nil),
}
