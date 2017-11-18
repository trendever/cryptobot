package main

import (
	"common/db"
	"common/rabbit"
	"core/proto"
	"errors"
	"github.com/shopspring/decimal"
	"lbapi"
)

func init() {
	rabbit.ServeRPC(proto.CheckKey, CheckKey)
	rabbit.ServeRPC(proto.OperatorByTg, OperatorByTg)
	rabbit.ServeRPC(proto.SetOperatorStatus, SetOperatorStatus)
	rabbit.ServeRPC(proto.SetOperatorKey, SetOperatorKey)
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

	s, p = op.Key.IsValid()
	return proto.Operator{
		ID:           op.ID,
		Username:     acc.Username,
		TelegramChat: op.TelegramChat,
		Status:       op.Status,
		HasValidKey:  s && p,
	}, nil
}

func OperatorByTg(chatID int64) (proto.Operator, error) {
	var op Operator
	scope := db.New().First(&op, "telegram_chat = ?", chatID)
	switch {
	case scope.RecordNotFound():
		return proto.Operator{
			Username: op.Username,
		}, nil

	case scope.Error != nil:
		return proto.Operator{}, scope.Error
	}

	s, p := op.Key.IsValid()
	// It's fine to return empty value.
	return proto.Operator{
		ID:           op.ID,
		Username:     op.Username,
		TelegramChat: chatID,
		Status:       op.Status,
		HasValidKey:  s && p,
	}, nil
}

func SetOperatorStatus(req proto.SetOperatorStatusRequest) (bool, error) {
	var op Operator
	err := db.New().First(&op, "telegram_chat = ?", req.ChatID).Error
	if err != nil {
		return false, err
	}
	// @TODO Ability to change status should depend no current status actuality
	op.Status = req.Status
	err = db.New().Save(&op).Error
	if err != nil {
		return false, err
	}
	return true, nil
}

func SetOperatorKey(req proto.SetOperatorKeyRequest) (proto.Operator, error) {
	p, s := req.Key.IsValid()
	if !p || !s {
		return proto.Operator{}, errors.New("invalid key")
	}
	acc, err := req.Key.Self()
	if err != nil {
		return proto.Operator{}, err
	}

	var op Operator
	scope := db.New().First(&op, "username = ?", acc.Username)
	switch {
	case scope.RecordNotFound():
		op.Username = acc.Username
		op.Deposit = decimal.Zero

	case scope.Error != nil:
		return proto.Operator{}, scope.Error

	default:
		if op.TelegramChat != req.ChatID {
			// @TODO send something to old chat and ensure unique chatID
		}
	}

	op.TelegramChat = req.ChatID
	op.Status = proto.OperatorStatus_Inactive
	op.Key = req.Key

	err = db.New().Save(&op).Error
	if err != nil {
		return proto.Operator{}, err
	}

	return proto.Operator{
		ID:           op.ID,
		Username:     acc.Username,
		TelegramChat: op.TelegramChat,
		Status:       op.Status,
		HasValidKey:  true,
	}, nil
}
