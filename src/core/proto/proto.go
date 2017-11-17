package proto

import (
	"common/rabbit"
	"lbapi"
)

type OperatorStatus int

const (
	// Account does not have valid keypair and does not perform any utility actions in the moment
	OperatorStatus_None OperatorStatus = 0
	// Account is valid but unready to accept offers.
	OperatorStatus_Inactive OperatorStatus = 1
	// Account is ready to accept offers.
	OperatorStatus_Ready OperatorStatus = 2
	// In action
	OperatorStatus_Busy    OperatorStatus = 3
	OperatorStatus_Utility OperatorStatus = 4
)

type Operator struct {
	ID           uint64
	Username     string
	TelegramChat int64
	HasValidKey  bool
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

type SetOperatorStatusRequest struct {
	ChatID int64
	Status OperatorStatus
}

var SetOperatorStatus = rabbit.RPC{
	Name:        "set_operator_status",
	HandlerType: (func(SetOperatorStatusRequest) (bool, error))(nil),
}

type SetOperatorKeyRequest struct {
	ChatID int64
	Key    lbapi.Key
}

var SetOperatorKey = rabbit.RPC{
	Name:        "set_operator_key",
	HandlerType: (func(SetOperatorKeyRequest) (Operator, error))(nil),
}
