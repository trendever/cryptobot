package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/qor/admin"
	"github.com/qor/qor"
	"github.com/qor/validations"
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
		value:  &Order{},
		config: &admin.Config{Name: "Order"},
		init: func(res *admin.Resource) {
			res.SearchAttrs(
				"ClientName",
			)
			res.IndexAttrs(
				"ID", "ClientName", "PaymentMethod", "FiatAmount", "Currency", "Status", "OperatorID",
			)

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
				Name:  "Mark finished",
				Modes: []string{"show", "menu_item"},
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
		},
	},
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
