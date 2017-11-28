package main

import (
	"common/cli"
	"common/config"
	"common/log"
	"common/rabbit"
	"common/soso"
	"crypto/sha512"
	"encoding/binary"
	"github.com/igm/sockjs-go/sockjs"
	"net/http"
	"time"
)

var conf struct {
	Rabbit rabbit.Config
	Listen string

	Debug     bool
	SentryDSN string

	Messages map[string]string
}

func main() {
	cli.Main(service{})
}

// SosoObj is soso controller
var SosoObj = soso.Default()

// Receiver is sockjs to soso adapter
func Receiver(session sockjs.Session) {
	SosoObj.RunReceiver(session)
}

type service struct{}

func (srv service) Load() {
	log.Fatal(config.LoadStruct("api", &conf))
	log.Init(conf.Debug, "api", conf.SentryDSN)
	log.Debug("config:\n%v", log.IndentEncode(conf))
}

func (srv service) Migrate(bool) {
	srv.Load()
	log.Info("nothing to migrate here, really")
}

func (srv service) Start() {
	srv.Load()
	rabbit.Start(&conf.Rabbit)
	soso.AddMiddleware(TokenMiddleware)
	SosoObj.HandleRoutes(SosoRoutes)
	http.Handle("/channel/", sockjs.NewHandler("/channel", sockjs.DefaultOptions, Receiver))
	http.ListenAndServe(conf.Listen, nil)
}

func (srv service) Cleanup() {
	log.Info("Shuting service down...")
	rabbit.Stop()
	log.Info("Service is stopped")
}

func IDForAddress(address string) uint64 {
	sum := sha512.Sum512([]byte(address))
	return binary.LittleEndian.Uint64(sum[:])
}

func TokenMiddleware(req *soso.Request, ctx *soso.Context, session soso.Session) error {
	if address, ok := req.TransMap["address"].(string); ok {
		ctx.Token = &soso.Token{UID: IDForAddress(address), Exp: time.Now().Add(time.Hour).Unix()}
	}
	return nil
}
