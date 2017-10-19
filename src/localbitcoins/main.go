package main

import (
	"common/config"
	"common/log"
	"common/proxy"
	"common/db"
	"localbitcoins/lbapi"
	"net/http"
	"common/cli"
	"time"
)

const ServiceName = "localbitcoins"

var conf struct {
	LBKey       lbapi.Key
	Proxy       string
	Debug       bool
	DumpQueries bool

	LBCheckTick string
	lbCheckTick time.Duration

	DB          db.Settings

	SentryDSN   string
}

type service struct {}

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
	LBCheckLoop()
}
