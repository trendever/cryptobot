package main

import (
	"common/cli"
	"common/config"
	"common/db"
	"common/log"
	"common/proxy"
	"common/rabbit"
	"lbapi"
	"net/http"
	"time"
)

const ServiceName = "core"

var conf struct {
	LBKey       lbapi.Key
	Proxy       string
	Debug       bool
	DumpQueries bool

	LBCheckTick string
	lbCheckTick time.Duration

	DB db.Settings

	Rabbit rabbit.Config

	SentryDSN string
}

var CurrencyList []string

type service struct{}

func main() {
	cli.Main(&service{})
}

func (srv service) Load() {
	log.Fatal(config.LoadStruct(ServiceName, &conf))
	log.Init(conf.Debug, ServiceName, conf.SentryDSN)
	log.Debug("config:\n%s", log.IndentEncode(conf))

	if conf.LBCheckTick == "" {
		conf.lbCheckTick = 5 * time.Second
	} else {
		var err error
		conf.lbCheckTick, err = time.ParseDuration(conf.LBCheckTick)
		if err != nil {
			log.Fatalf("invalid LBCheckTick provided in config")
		}
	}

	db.Init(&conf.DB)
}

func (srv service) Migrate(drop bool) {
	srv.Load()
	migrate(drop)
}

func (srv service) Cleanup() {}

func (srv service) Start() {
	srv.Load()

	transport, err := proxy.TransportFromURL(conf.Proxy)
	log.Fatal(err)
	lbapi.HTTPCli = &http.Client{
		Transport: transport,
	}
	lbapi.DumpQueries = conf.Debug

	_, err = conf.LBKey.Wallet()
	if err != nil {
		log.Fatalf("failed to init-check buffer wallet: %v", err)
	}
	// I think load it just on start will be enough
	CurrencyList, err = conf.LBKey.CurrencyList()
	if err != nil {
		log.Fatalf("failed to load currency list: %v", err)
	}

	rabbit.Start(&conf.Rabbit)

	go LBTransactionsLoop()
}
