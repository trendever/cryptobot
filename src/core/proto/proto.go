package proto

import (
	"common/rabbit"
	"lbapi"
)

type Operator struct {
	ID           uint64
	Username     string
	TelegramChat uint64
}

var CheckKey = rabbit.RPC{
	Name:        "check_lb_key",
	HandlerType: (func(lbapi.Key) (Operator, error))(nil),
}
