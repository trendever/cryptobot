package main

import (
	"common/log"
	"common/proxy"
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
	list, err := key.ByOnlineList("USD")
	log.Debug("%v\nlen: %v, err: %v", log.IndentEncode(list), len(list), err)
}
