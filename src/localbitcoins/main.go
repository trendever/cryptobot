package main

import (
	"common/log"
	"common/proxy"
	"github.com/shopspring/decimal"
	"localbitcoins/lbapi"
	"net/http"
	"os"
)

func main() {
	log.Init(true, "WAT", "")
	tranport, err := proxy.TransportFromURL("socks5://localhost:4700")
	lbapi.HTTPCli = &http.Client{
		Transport: tranport,
	}
	lbapi.DumpQueries = true
	key := lbapi.Key{
		Public: os.Args[1],
		Secret: os.Args[2],
	}
	err = key.CreateInvoice("RUB", decimal.NewFromFloat(10000), "WAT", true, "")
	log.Debug("err: %v", err)
}
