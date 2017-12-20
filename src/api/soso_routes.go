package main

import (
	"common/rabbit"
	"common/soso"
	"core/proto"
	"errors"
	"github.com/shopspring/decimal"
	"net/http"
)

var SosoRoutes = []soso.Route{
	{
		Domain:  "order",
		Method:  "create",
		Handler: CreateOrderHandler,
	},
	{
		Domain:  "order",
		Method:  "get",
		Handler: GetOrderHandler,
	},
	{
		Domain:  "order",
		Method:  "cancel",
		Handler: CancelOrderHandler,
	},
	{
		Domain:  "order",
		Method:  "mark_payed",
		Handler: MarkPayedHandler,
	},
}

func GetOrderHandler(c *soso.Context, arg *struct {
	OrderID uint64 `json:"order_id"`
	Addess  string `json:"address"`
}) {
	if arg.OrderID == 0 {
		c.ErrorResponse(http.StatusBadRequest, soso.LevelError, errors.New("bad request"))
		return
	}

	order, err := GetOrder(arg.OrderID)

	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	if order.Destination != arg.Addess {
		c.ErrorResponse(http.StatusForbidden, soso.LevelError, errors.New("forbidden"))
		return
	}

	c.SuccessResponse(order)
}

func CreateOrderHandler(c *soso.Context, arg *struct {
	ClientName    string          `json:"client_name"`
	Address       string          `json:"address"`
	PaymentMethod string          `json:"payment_method"`
	Currency      string          `json:"currency"`
	FiatAmount    decimal.Decimal `json:"fiat_amount"`
}) {
	if arg.ClientName == "" || arg.Address == "" ||
		arg.PaymentMethod == "" || arg.Currency == "" || arg.FiatAmount.Sign() <= 0 {
		c.ErrorResponse(http.StatusBadRequest, soso.LevelError, errors.New("bad request"))
		return
	}

	order, err := CreateOrder(proto.Order{
		ClientName:    arg.ClientName,
		Destination:   arg.Address,
		PaymentMethod: arg.PaymentMethod,
		Currency:      arg.Currency,
		FiatAmount:    arg.FiatAmount,
	})

	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	c.SuccessResponse(order)
}

func CancelOrderHandler(c *soso.Context, arg *struct {
	OrderID uint64 `json:"order_id"`
	Address string `json:"address"`
}) {
	if arg.OrderID == 0 {
		c.ErrorResponse(http.StatusBadRequest, soso.LevelError, errors.New("bad request"))
		return
	}

	order, err := GetOrder(arg.OrderID)
	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	if order.Destination != arg.Address {
		c.ErrorResponse(http.StatusForbidden, soso.LevelError, errors.New("forbidden"))
		return
	}

	_, err = CancelOrder(order.ID)
	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	c.SuccessResponse("success")
}

func MarkPayedHandler(c *soso.Context, arg *struct {
	OrderID uint64 `json:"order_id"`
	Address string `json:"address"`
}) {
	if arg.OrderID == 0 {
		c.ErrorResponse(http.StatusBadRequest, soso.LevelError, errors.New("bad request"))
		return
	}

	order, err := GetOrder(arg.OrderID)
	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	if order.Destination != arg.Address {
		c.ErrorResponse(http.StatusForbidden, soso.LevelError, errors.New("forbidden"))
		return
	}

	_, err = MarkPayed(order.ID)
	if err != nil {
		err := err.(rabbit.RPCError)
		if err.Kind != rabbit.RPCError_Forwarded {
			c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, errors.New("service unavailable"))
			return
		}
		c.ErrorResponse(http.StatusInternalServerError, soso.LevelError, err)
		return
	}

	c.SuccessResponse("success")
}
