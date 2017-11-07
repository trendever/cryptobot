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
	rabbit.ServeRPC(proto.OperatorByTg, OperatorByTg)
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

	var op Operator
	scope := db.New().First(&op, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		return proto.Operator{
			Username: acc.Username,
		}, nil
	case scope.Error != nil:
		return proto.Operator{}, scope.Error
	}

	return proto.Operator{
		ID:           op.ID,
		Username:     acc.Username,
		TelegramChat: op.TelegramChat,
		Status:       op.Status,
	}, nil
}

func OperatorByTg(chatID int64) (proto.Operator, error) {
	var op Operator
	err := db.New().First(&op, "chat_id = ?", chatID).Error
	// It's fine to return empty value.
	return proto.Operator{
		ID:           op.ID,
		Username:     op.Username,
		TelegramChat: chatID,
		Status:       op.Status,
	}, err
}
