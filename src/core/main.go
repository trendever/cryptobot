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

	QorAddress string

	// List of currencies which rates are fetched automatically since service start.
	// Others will be added on demand(when first order occurs)
	PrefetchRates    []string
	RatesRefreshTick string

	Messages map[string]string

	LBCheckTick      time.Duration
	OrdersUpdateTick time.Duration

	OperatorFee float64
	BotFee      float64

	DB     db.Settings
	Rabbit rabbit.Config

	TelegramChanel string

	SentryDSN string

	OrderTimeouts struct {
		Accept  time.Duration
		Payment time.Duration
		Confirm time.Duration
	}
}

var (
	CurrencyList []string
	// current lb buffer account
	LBSelf lbapi.Account
	// bitcoin(lb one) address for refill if deposits
	ReceivingAddress string
)

type service struct{}

func main() {
	cli.Main(&service{})
}

func (srv service) Load() {
	log.Fatal(config.LoadStruct(ServiceName, &conf))
	log.Init(conf.Debug, ServiceName, conf.SentryDSN)
	log.Debug("config:\n%s", log.IndentEncode(conf))

	if conf.LBCheckTick == 0 {
		conf.LBCheckTick = 5 * time.Second
	}
	if conf.OrdersUpdateTick == 0 {
		conf.OrdersUpdateTick = 5 * time.Second
	}
	t := conf.OrderTimeouts
	if t.Accept < time.Minute || t.Payment < time.Minute || t.Confirm < time.Minute {
		log.Fatalf("invalid order timeouts")
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
	lbapi.DumpQueries = conf.DumpQueries

	LBSelf, err = conf.LBKey.Self()
	if err != nil {
		log.Fatalf("failed to load lb account: %v", err)
	}
	log.Info("lb buffer username is '%v'", LBSelf.Username)

	wallet, err := conf.LBKey.Wallet()
	if err != nil {
		log.Fatalf("failed to init-check buffer wallet: %v", err)
	}
	ReceivingAddress = wallet.ReceivingAddress

	// I think load it just on start will be enough
	CurrencyList, err = conf.LBKey.CurrencyList()
	if err != nil {
		log.Fatalf("failed to load currency list: %v", err)
	}

	QorInit()

	go RatesRefresh(conf.PrefetchRates)

	rabbit.Start(&conf.Rabbit)

	go LBTransactionsLoop()
	StartOrderManager()
}

func M(key string) string {
	msg, ok := conf.Messages[key]
	if ok {
		return msg
	}
	log.Warn("message for key '%v' is undefined", key)
	return key
}
