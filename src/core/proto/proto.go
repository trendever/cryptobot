package proto

import (
	"common/rabbit"
	"github.com/shopspring/decimal"
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

type OrderStatus int

const (
	OrderStatus_New OrderStatus = 1
	// There is no enough funds on bitshares buffer(taking in account locked some)
	OrderStatus_Unrealizable OrderStatus = 2
	// There was no operators who can/want to take order
	OrderStatus_Rejected OrderStatus = 3
	// Operator took order
	OrderStatus_Accepted OrderStatus = 4
	// Waiting for payment from client
	OrderStatus_Payment OrderStatus = 5
	// Canceled by client
	OrderStatus_Canceled OrderStatus = 6
	// Client did not fund lb contract in time
	OrderStatus_Timeout OrderStatus = 7
	// Waiting for confirmation from operator
	OrderStatus_Confirmation OrderStatus = 8
	// Transferring bitshares
	OrderStatus_Transfer OrderStatus = 9
	// Finished
	OrderStatus_Finished OrderStatus = 10
)

type Order struct {
	ID         uint64
	ClientName string
	// Bitshares address
	Destination    string
	PaymentMethods string
	Currency       string
	// In currency above
	FiatAmount decimal.Decimal
	// Value of lb contract
	LBAmount decimal.Decimal
	// @TODO commission-related fields?
	Status OrderStatus
}
