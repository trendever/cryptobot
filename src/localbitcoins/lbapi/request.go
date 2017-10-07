package lbapi

import (
	"bytes"
	"common/log"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/shopspring/decimal"
	"io"
	"io/ioutil"
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
	split := strings.Split(endpoint, "?")
	if len(split) == 2 {
		endpoint = split[0]
		args = split[1]
	}
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

func (key Key) DecodedRequest(method, endpoint string, args string, out interface{}) (nextPage string, err error) {
	var result = struct {
		Data       interface{} `json:"data"`
		Pagination struct {
			Next string `json:"next"`
		} `json:"pagination"`
		Error struct {
			Code    int64  `json:"error_code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Data: out}
	for attempt := 0; attempt < 3; attempt++ {
		var resp *http.Response
		resp, err = key.RawRequest(method, endpoint, args)
		if err != nil || resp.StatusCode == 500 {
			time.Sleep(time.Second / 5)
			continue
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		err = json.Unmarshal(body, &result)
		if err != nil {
			return "", err
		}
		switch result.Error.Code {
		case 0:
			return strings.TrimPrefix(result.Pagination.Next, BASE_URL), nil
		// "nonce was too small". probably we maneged to send multiple requests in almost same time
		case 42:
			time.Sleep(time.Millisecond)
			continue
		default:
			return "", errors.New(result.Error.Message)
		}
	}
	return
}

type Advertisement struct {
	Data struct {
		ID                   uint64    `json:"ad_id"`
		CreatedAt            time.Time `json:"created_at"`
		Visible              bool      `json:"visible"`
		HiddenByOpeningHours bool      `json:"hidden_by_opening_hours"`
		LocationString       string    `json:"location_string"`
		CountryCode          string    `json:"countrycode"`
		City                 string    `json:"sity"`
		Lat                  float64   `json:"lat"`
		Lon                  float64   `json:"lon"`
		// docs say:
		// >often one of LOCAL_SELL, LOCAL_BUY, ONLINE_SELL, ONLINE_BUY
		// often?
		TradeType string `json:"trade_type"`
		Currency  string `json:"currency"`
		// it is payment method actuality. "SPECIFIC_BANK"/"QIWI"/"INTERNATIONAL_WIRE_SWIFT"/etc
		OnlineProvider string `json:"online_provider"`
		// there is no specific format
		BankName string `json:"bank_name"`
		// i have no idea what is it
		FirstTimeLimitBTC decimal.Decimal `json:"first_time_limit_btc"`
		// no idea again
		VolumeCoefficientBTC decimal.Decimal `json:"volume_coefficient_btc"`
		// what?
		ReferenceType    string `json:"reference_type"`
		DisplayReference bool   `json:"display_reference"`
		// @CHECK three values below can be null. Is it important?
		// in denominated currency
		MinAmount          decimal.Decimal `json:"min_amount"`
		MaxAmount          decimal.Decimal `json:"max_amount"`
		MaxAmountAvailable decimal.Decimal `json:"max_amount_available"`
		// >"5,10,20"
		// wtf?
		LimitToFiatAmounts string `json:"limit_to_fiat_amounts"`
		// current price per BTC in USD
		TempPriceUSD decimal.Decimal `json:"temp_price_usd"`
		// >boolean if LOCAL_SELL
		// wat?
		Floating bool `json:"floating"`

		Profile struct {
			Username   string    `json:"username"`
			LastOnline time.Time `json:"last_online"`
			// can contain bs values like "N/A" or "10 000+"
			TradeCount    string `json:"trade_count"`
			FeedbackScore int64  `json:"feedback_score"`
			// >username, trade count and feedback score combined
			Combined string `json:"name"`
		} `json:"profile"`

		RequireFeedbackScore       int64           `json:"require_feedback_score"`
		RequireTradeVolume         decimal.Decimal `json:"require_trade_volume"`
		RequireIdentification      bool            `json:"require_identification"`
		SMSVerificationRequired    bool            `json:"sms_verification_required"`
		TrustedRequired            bool            `json:"trusted_required"`
		RequireTrustedByAdvertiser bool            `json:"require_trusted_by_advertiser"`
		PaymentWindowMinutes       int64           `json:"payment_window_minutes"`
		// >track_max_amount is the same as the advertisement option "Track liquidity" on web site.
		TrackMaxAmount bool   `json:"track_max_amount"`
		ATMModel       string `json:"atm_model"`
		Email          string `json:"email"`
		Message        string `json:"msg"`

		// @NOTE there is some more fields for ad owner
	} `json:"data"`
	Actions struct {
		// url for ad page
		PublicView string `json:"public_view"`

		// @NOTE there is some more fields for ad owner
	} `json:"actions"`
}

func (key Key) ByOnlineList(currency string) ([]Advertisement, error) {
	var ret []Advertisement
	uri := fmt.Sprintf("/buy-bitcoins-online/%s/.json", currency)
	for {
		var result struct {
			List  []Advertisement `json:"ad_list"`
			Count uint64          `json:"ad_count"`
		}
		next, err := key.DecodedRequest("GET", uri, "", &result)
		if err != nil {
			return ret, err
		}
		ret = append(ret, result.List...)
		if next == "" {
			break
		}
		uri = next
	}
	return ret, nil
}
