package main

import (
	"common/db"
	"common/rabbit"
	"core/proto"
	"errors"
	"lbapi"
)

func init() {
	rabbit.ServeRPC(proto.CheckKey, CheckKey)
}

func CheckKey(key lbapi.Key) (proto.Operator, error) {
	p, s := key.IsValid()
	if !p || !s {
		return proto.Operator{}, errors.New("invalid key")
	}
	acc, err := key.Self()
	if err != nil {
		return proto.Operator{}, err
	}

	var operator Operator
	scope := db.New().First(&operator, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		return proto.Operator{
			Username: acc.Username,
		}, nil
	case scope.Error != nil:
		return proto.Operator{}, scope.Error
	}

	return proto.Operator{
		ID:           operator.ID,
		Username:     acc.Username,
		TelegramChat: operator.TelegramChat,
	}, nil
}
