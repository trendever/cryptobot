package main

import (
	"common/db"
	"common/log"
)

var models = []interface{}{
	&LBTransaction{},
	&Operator{},
	&Order{},
}

func migrate(drop bool) {
	tx := db.NewTransaction()

	if drop {
		log.Fatal(tx.DropTableIfExists(models...).Error)
	}

	log.Fatal(tx.AutoMigrate(models...).Error)

	log.Fatal(tx.Model(&LBTransaction{}).AddUniqueIndex("unique_transaction",
		"created_at", "direction", "amount", "description").Error)

	log.Fatal(tx.Commit().Error)
}
