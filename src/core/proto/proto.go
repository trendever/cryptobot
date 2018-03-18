package proto

import (
	"common/rabbit"
	"github.com/shopspring/decimal"
	"lbapi"
	"time"
)

type OperatorStatus int

var (
	DBError              = "db error"
	ForbiddenError       = "forbidden"
	ContactNotFoundError = "contact not found"
)

const DepositTransactionPrefix = "DEPO_"

const (
	// Account does not have valid keypair and does not perform any utility actions in the moment
	OperatorStatus_None OperatorStatus = 0
	// Account is valid but unready to accept offers.
	OperatorStatus_Inactive OperatorStatus = 1
	// Account is ready to accept offers.
	OperatorStatus_Ready OperatorStatus = 2
	// Waiting for accent/reject offer
	OperatorStatus_Proposal OperatorStatus = 4
	// In action
	OperatorStatus_Busy    OperatorStatus = 5
	OperatorStatus_Utility OperatorStatus = 6
)

var OperatorStatusStrings = map[OperatorStatus]string{
	OperatorStatus_None:     "none",
	OperatorStatus_Inactive: "inactive",
	OperatorStatus_Ready:    "ready",
	OperatorStatus_Proposal: "proposal",
	OperatorStatus_Busy:     "busy",
	OperatorStatus_Utility:  "utility",
}

func (s OperatorStatus) String() string {
	return OperatorStatusStrings[s]
}

type Operator struct {
	ID           uint64
	Username     string
	TelegramChat int64
	HasValidKey  bool
	Status       OperatorStatus
	CurrentOrder uint64
	Deposit      decimal.Decimal
}

var CheckKey = rabbit.RPC{
	Name:        "check_lb_key",
	Concurrent:  true,
	HandlerType: (func(lbapi.Key) (Operator, error))(nil),
}

var OperatorByTg = rabbit.RPC{
	Name:        "operator_by_tg",
	Concurrent:  true,
	HandlerType: (func(chatID int64) (Operator, error))(nil),
}

var OperatorByID = rabbit.RPC{
	Name:        "operator_by_id",
	Concurrent:  true,
	HandlerType: (func(operatorID uint64) (Operator, error))(nil),
}

type SetOperatorStatusRequest struct {
	ChatID int64
	Status OperatorStatus
}

var SetOperatorStatus = rabbit.RPC{
	Name:        "set_operator_status",
	Concurrent:  true,
	HandlerType: (func(SetOperatorStatusRequest) (bool, error))(nil),
}

type SetOperatorKeyRequest struct {
	ChatID int64
	Key    lbapi.Key
}

var SetOperatorKey = rabbit.RPC{
	Name:        "set_operator_key",
	Timeout:     5 * time.Second,
	HandlerType: (func(SetOperatorKeyRequest) (Operator, error))(nil),
}

var GetDepositRefillAddress = rabbit.RPC{
	Name:        "get_deposi_refill_address",
	Concurrent:  true,
	HandlerType: (func(operatorID uint64) (string, error))(nil),
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
	// Operator dropped order after accepting it but before requisites was sent to client.
	OrderStatus_Dropped OrderStatus = 5
	// Related lb contact found
	OrderStatus_Linked OrderStatus = 6
	// Waiting for payment from client
	OrderStatus_Payment OrderStatus = 7
	// Canceled by client
	OrderStatus_Canceled OrderStatus = 8
	// Client did not fund lb contract in time
	OrderStatus_Timeout OrderStatus = 9
	// Waiting for confirmation from operator or lb
	OrderStatus_Confirmation OrderStatus = 10
	// Transferring bitshares
	OrderStatus_Transfer OrderStatus = 11
	// Finished
	OrderStatus_Finished OrderStatus = 12
)

var OrderStatusStrings = map[OrderStatus]string{
	OrderStatus_New:          "new",
	OrderStatus_Unrealizable: "unrealizable",
	OrderStatus_Rejected:     "rejected",
	OrderStatus_Accepted:     "accepted",
	OrderStatus_Dropped:      "dropped",
	OrderStatus_Linked:       "linked",
	OrderStatus_Payment:      "payment",
	OrderStatus_Canceled:     "canceled",
	OrderStatus_Timeout:      "timeout",
	OrderStatus_Confirmation: "confirmation",
	OrderStatus_Transfer:     "transfer",
	OrderStatus_Finished:     "finished",
}

func (s OrderStatus) String() string {
	return OrderStatusStrings[s]
}

type Order struct {
	ID         uint64
	ClientName string
	// Bitshares address
	Destination   string
	PaymentMethod string
	Currency      string
	// In currency above
	FiatAmount        decimal.Decimal
	PaymentRequisites string
	LBContractID      uint64
	// Value of lb contract in BTC
	LBAmount    decimal.Decimal
	LBFee       decimal.Decimal
	OperatorFee decimal.Decimal
	BotFee      decimal.Decimal

	Status     OrderStatus
	OperatorID uint64
}

var OrderEventRoute = rabbit.Route{
	{
		Node: rabbit.Exchange{
			Name:    "order_event",
			Kind:    "fanout",
			Durable: true,
		},
	},
	{
		Keys: []string{""},
		Node: rabbit.Queue{
			Name:       "",
			Exclusive:  true,
			AutoDelete: true,
		},
	},
}

var CreateOrder = rabbit.RPC{
	Name:        "create_order",
	Concurrent:  true,
	HandlerType: (func(Order) (Order, error))(nil),
	Timeout:     time.Second * 20,
}

var CancelOrder = rabbit.RPC{
	Name:        "cancel_order",
	Concurrent:  true,
	HandlerType: (func(orderID uint64) (bool, error))(nil),
	Timeout:     time.Second * 5,
}

var GetOrder = rabbit.RPC{
	Name:        "get_order",
	Concurrent:  true,
	HandlerType: (func(id uint64) (Order, error))(nil),
}

type AcceptOfferRequest struct {
	OperatorID uint64
	OrderID    uint64
}

var AcceptOffer = rabbit.RPC{
	Name:        "accept_offer",
	Concurrent:  true,
	HandlerType: (func(AcceptOfferRequest) (Order, error))(nil),
}

type SkipOfferRequest struct {
	OperatorID uint64
	OrderID    uint64
}

var SkipOffer = rabbit.RPC{
	Name:        "skip_offer",
	Concurrent:  true,
	HandlerType: (func(SkipOfferRequest) (bool, error))(nil),
}

type DropOrderRequest struct {
	OperatorID uint64
	OrderID    uint64
}

// Drop accepted order
var DropOrder = rabbit.RPC{
	Name:        "drop_order",
	Concurrent:  true,
	HandlerType: (func(DropOrderRequest) (bool, error))(nil),
}

type LinkLBContractRequest struct {
	OrderID    uint64
	Requisites string
}

var LinkLBContact = rabbit.RPC{
	Name:        "link_lb_contact",
	Concurrent:  true,
	HandlerType: (func(LinkLBContractRequest) (Order, error))(nil),
	Timeout:     time.Second * 10,
}

var RequestPayment = rabbit.RPC{
	Name:        "request_payment",
	Concurrent:  true,
	HandlerType: (func(orderID uint64) (Order, error))(nil),
}

var MarkPayed = rabbit.RPC{
	Name:        "mark_payed",
	Concurrent:  true,
	HandlerType: (func(orderID uint64) (bool, error))(nil),
}

var ConfirmPayment = rabbit.RPC{
	Name:        "confirm_payment",
	Concurrent:  true,
	HandlerType: (func(orderID uint64) (bool, error))(nil),
}
