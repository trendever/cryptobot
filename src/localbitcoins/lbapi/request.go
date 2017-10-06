package lbapi

import (
	"bytes"
	"common/log"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"time"
)

const (
	BASE_URL = "https://localbitcoins.com"
)

var (
	HTTPCli     = http.DefaultClient
	DumpQueries = false
)

type Key struct {
	Public string
	Secret string
}

func (key Key) RawRequest(method, endpoint string, args string) (*http.Response, error) {
	url := BASE_URL + endpoint
	nonce := strconv.FormatInt(time.Now().UnixNano()/100, 10)
	data := nonce + key.Public + endpoint + args
	hash := hmac.New(sha256.New, []byte(key.Secret))
	hash.Write([]byte(data))
	sign := strings.ToUpper(hex.EncodeToString(hash.Sum(nil)))

	var bodyReader io.Reader
	if args != "" {
		switch method {
		case "GET":
			url += "?" + args
		case "POST":
			bodyReader = bytes.NewReader([]byte(args))
		default:
			return nil, errors.New("unsupported method")
		}
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Apiauth-Key", key.Public)
	req.Header.Set("Apiauth-Nonce", nonce)
	req.Header.Set("Apiauth-Signature", string(sign))
	if DumpQueries {
		dump, _ := httputil.DumpRequest(req, true)
		log.Debug(string(dump))
	}
	resp, err := HTTPCli.Do(req)
	if err != nil {
		return nil, err
	}
	if DumpQueries {
		dump, _ := httputil.DumpResponse(resp, true)
		log.Debug(string(dump))
	}
	return resp, err
}
