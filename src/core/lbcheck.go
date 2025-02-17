package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"fmt"
	"github.com/jinzhu/gorm"
	"lbapi"
	"strconv"
	"strings"
	"time"
)

func LBTransactionsLoop() {
	for range time.Tick(conf.LBCheckTick) {
		wallet, err := conf.LBKey.Wallet()
		if err != nil {
			log.Error(err)
			continue
		}

		for _, tx := range wallet.Received {
			log.Error(ProcessIncomingTx(tx))
		}
		for _, tx := range wallet.Sent {
			log.Error(SaveOutgoingTx(tx))
		}
	}
}

func ProcessIncomingTx(event lbapi.Transaction) error {
	// i'm not sure whether we can rely on lb log consistence, so it's better to try save everything every time
	tx := db.NewTransaction().Set("gorm:insert_option", "ON CONFLICT DO NOTHING")
	data := LBTransaction{
		Account:     LBSelf.Username,
		Direction:   TransactionDirection_To,
		Transaction: event,
	}
	err := tx.Create(&data).Error
	switch {
	case err == nil:
	// gorm does not know how to handle "on conflict",
	// this error means that current transaction was already saved earlier
	case err.Error() == "sql: no rows in result set":
		tx.Rollback()
		return nil
	default:
		return err
	}

	if !strings.HasPrefix(event.Description, proto.DepositTransactionPrefix) {
		return tx.Commit().Error
	}

	operatorStr := strings.TrimPrefix(event.Description, proto.DepositTransactionPrefix)
	operatorID, err := strconv.ParseUint(operatorStr, 36, 64)
	if err != nil {
		log.Warn("invalid account id '%v' it transaction %v", operatorStr, data.ID)
		// transaction itself should be saved in any case
		return tx.Commit().Error
	}
	log.Debug("new deposit for %v: %v", operatorID, data.Amount)

	var op Operator
	res := tx.First(&op, "id = ?", operatorID)
	switch {
	case res.RecordNotFound():
		log.Warn("found deposit transaction %v with unknown operator id", data.ID)
		return tx.Commit().Error
	case res.Error != nil:
		tx.Rollback()
		return res.Error
	}

	err = tx.Model(&op).Update("deposit", gorm.Expr("deposit + ?", event.Amount)).Error
	if err != nil {
		tx.Rollback()
		return err
	}
	err = tx.Commit().Error
	if err != nil {
		return err
	}
	if op.Status == proto.OperatorStatus_Ready {
		manager.PushOperator(op.ID, true)
	}

	go func() {
		err := SendTelegramNotify(strconv.FormatInt(op.TelegramChat, 10), fmt.Sprintf(
			M("balance notify %v"), event.Amount,
		), false)
		if err != nil {
			log.Errorf("failed to send balance notify: %v", err)
		}
	}()
	return nil
}

func SaveOutgoingTx(event lbapi.Transaction) error {
	err := db.New().Set("gorm:insert_option", "ON CONFLICT DO NOTHING").Create(&LBTransaction{
		Account:     LBSelf.Username,
		Direction:   TransactionDirection_From,
		Transaction: event,
	}).Error
	if err == nil || err.Error() == "sql: no rows in result set" {
		return nil
	}
	return err
}
