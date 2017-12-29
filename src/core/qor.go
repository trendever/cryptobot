package main

import (
	"common/db"
	"common/log"
	"core/proto"
	"github.com/jinzhu/gorm"
	"github.com/qor/admin"
	"github.com/qor/qor"
	"github.com/qor/validations"
	"net/http"
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
