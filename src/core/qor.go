package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"errors"
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/qor/admin"
	"github.com/qor/qor"
	"github.com/qor/roles"
	"github.com/qor/validations"
	"github.com/shopspring/decimal"
	"net/http"
	"reflect"
	"sort"
)

func QorInit() {
	db := db.New()
	validations.RegisterCallbacks(db)
	adm := admin.New(&qor.Config{
		DB: db,
	})

	adm.SetAuth(DummyAuth{})

	adm.AddMenu(&admin.Menu{Name: "Dashboard", Link: "/admin"})

	for _, res := range resources {
		res.res = adm.AddResource(res.value, res.config)
	}
	for _, res := range resources {
		if res.init != nil {
			res.init(res.res)
		}
	}

	adm.MountTo("/admin", http.DefaultServeMux)
	go func() {
		log.Fatal(http.ListenAndServe(conf.QorAddress, http.DefaultServeMux))
	}()
}

type resource struct {
	value  interface{}
	config *admin.Config
	res    *admin.Resource
	init   func(*admin.Resource)
}

var resources = []*resource{
	{
		value: &Order{},
		config: &admin.Config{
			Name: "Order",
			Permission: roles.Deny(roles.Delete, roles.Anyone).
				Deny(roles.Create, roles.Anyone).Deny(roles.Update, roles.Anyone),
		},
		init: ordersInit,
	},
	{
		value: &Operator{},
		config: &admin.Config{
			Name:       "Operator",
			Permission: roles.Deny(roles.Delete, roles.Anyone).Deny(roles.Create, roles.Anyone),
		},
		init: operatorsInit,
	},
	{
		value: &LBTransaction{},
		config: &admin.Config{
			Name: "LBTransaction",
			Permission: roles.Deny(roles.Delete, roles.Anyone).
				Deny(roles.Create, roles.Anyone).Deny(roles.Update, roles.Anyone),
		},
		init: lbTransactionsInit,
	},
}

func lbTransactionsInit(res *admin.Resource) {
	res.IndexAttrs(
		"ID", "CreatedAt", "Account", "Direction", "Amount", "Description",
	)
	res.ShowAttrs(&admin.Section{
		Rows: [][]string{
			{"Account", "Direction"},
			{"CreatedAt", "Type"},
			{"Amount", "Description"},
			{"BitcoinTx"},
		},
	})
}

func operatorsInit(res *admin.Resource) {
	res.SearchAttrs(
		"Username",
	)
	res.IndexAttrs(
		"ID", "Username", "Deposit", "Status", "CurrentOrder",
	)
	res.ShowAttrs("-Public", "-Secret")
	res.EditAttrs("Note")

	res.Meta(&admin.Meta{
		Name: "Note",
		Type: "text",
	})
	// Make sure nothing but note can be saved(to avoid possible races)
	res.SaveHandler = func(val interface{}, ctx *qor.Context) error {
		op, ok := val.(*Operator)
		if !ok {
			return errors.New("unxepected record type")
		}
		return db.New().Model(op).Update("note", op.Note).Error
	}

	statuses := make([]int, 0, len(proto.OperatorStatusStrings))
	for status := range proto.OperatorStatusStrings {
		statuses = append(statuses, int(status))
	}
	sort.Ints(statuses)
	for _, status := range statuses {
		scp := status
		res.Scope(&admin.Scope{
			Name:  proto.OperatorStatus(scp).String(),
			Group: "Status",
			Handler: func(db *gorm.DB, context *qor.Context) *gorm.DB {
				return db.Where("status = ?", scp)
			},
		})
	}

	type depoArg struct {
		Amount  decimal.Decimal
		Comment string
	}
	depoArgRes := res.GetAdmin().NewResource(&depoArg{})

	updateDepo := func(records []interface{}, arg *depoArg, writeOff bool) error {
		if arg.Amount.Sign() <= 0 || arg.Comment == "" {
			return errors.New("invalid argument")
		}

		if writeOff {
			arg.Amount = arg.Amount.Neg()
		}
		tx := db.NewTransaction()
		for _, record := range records {
			op, ok := record.(*Operator)
			if !ok {
				tx.Rollback()
				return errors.New("unxepected record type")
			}
			err := tx.Model(&op).Update("deposit", gorm.Expr("deposit + ?", arg.Amount)).Error
			if err != nil {
				tx.Rollback()
				return err
			}
		}
		err := tx.Commit().Error
		if err != nil {
			return err
		}
		for _, record := range records {
			op := record.(*Operator)
			log.Info("Deposit of operator %v(%v) was changed in qor for amount %v with comment '%v'", op.ID, op.Username, arg.Amount, arg.Comment)
		}
		return nil
	}

	res.Action(&admin.Action{
		Name:     "Add to deposit",
		Resource: depoArgRes,
		Modes:    []string{"show", "menu_item"},
		Handler: func(argument *admin.ActionArgument) error {
			arg, ok := argument.Argument.(*depoArg)
			if !ok {
				return errors.New("unxepected argument type")
			}
			return updateDepo(argument.FindSelectedRecords(), arg, false)
		},
	})
	res.Action(&admin.Action{
		Name:     "Write-off from deposit",
		Resource: depoArgRes,
		Modes:    []string{"show", "menu_item"},
		Handler: func(argument *admin.ActionArgument) error {
			arg, ok := argument.Argument.(*depoArg)
			if !ok {
				return errors.New("unxepected argument type")
			}
			return updateDepo(argument.FindSelectedRecords(), arg, true)
		},
	})
}

func ordersInit(res *admin.Resource) {
	res.SearchAttrs(
		"ClientName",
	)
	res.Meta(&admin.Meta{
		Name: "OutletAmount",
		Valuer: func(val interface{}, _ *qor.Context) interface{} {
			order, ok := val.(*Order)
			if !ok {
				return ""
			}
			return order.OutletAmount()
		},
	})
	res.IndexAttrs(
		"ID", "CreatedAt", "ClientName", "PaymentMethod", "FiatAmount", "Currency", "Status", "OperatorID",
	)

	res.ShowAttrs(&admin.Section{
		Rows: [][]string{
			{"ID", "CreatedAt"},
			{"Status", "OperatorID"},
			{"ClientName", "PaymentMethod"},
			{"Destination"},
			{"FiatAmount", "Currency"},
			{"LBAmount", "OutletAmount"},
			{"LBFee", "OperatorFee"},
			{"BotFee"},
		},
	})

	statuses := make([]int, 0, len(proto.OrderStatusStrings))
	for status := range proto.OrderStatusStrings {
		statuses = append(statuses, int(status))
	}
	sort.Ints(statuses)
	for _, status := range statuses {
		scp := status
		res.Scope(&admin.Scope{
			Name:  proto.OrderStatus(scp).String(),
			Group: "Status",
			Handler: func(db *gorm.DB, context *qor.Context) *gorm.DB {
				return db.Where("status = ?", scp)
			},
		})
	}

	res.Action(&admin.Action{
		Name:       "Mark finished",
		Modes:      []string{"show", "menu_item"},
		Permission: roles.Allow(roles.Update, roles.Anyone),
		Handler: func(arg *admin.ActionArgument) error {
			for _, record := range arg.FindSelectedRecords() {
				order, ok := record.(*Order)
				if !ok {
					return fmt.Errorf("unexpected type %v in mark finished qor action", reflect.TypeOf(record))
				}
				if order.Status != proto.OrderStatus_Transfer {
					return fmt.Errorf("order have unexpected status '%v'", order.Status)
				}
				order.Status = proto.OrderStatus_Finished
				err := order.Save(arg.Context.DB)
				if err != nil {
					return fmt.Errorf("failed to save order: %v", err)
				}
			}
			return nil
		},
		Visible: func(record interface{}, context *admin.Context) bool {
			order, ok := record.(*Order)
			if !ok {
				log.Errorf("unexpected type %v in visible check of 'mark finished' qor action", reflect.TypeOf(record))
				return false
			}
			return order.Status == proto.OrderStatus_Transfer
		},
	})
}

type DummyAuth struct{}

func (DummyAuth) LoginURL(c *admin.Context) string {
	return "/"
}
func (DummyAuth) LogoutURL(c *admin.Context) string {
	return "/"
}
func (DummyAuth) GetCurrentUser(c *admin.Context) qor.CurrentUser {
	return DummyUser{}
}

type DummyUser struct{}

func (u DummyUser) DisplayName() string {
	return "user"
}
