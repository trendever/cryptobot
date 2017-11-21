package main

import (
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"sync"
	"time"
)

const RateRefreshTimeout = 5 * time.Minute

// @TODO Periodic check in separate loop. Our orders do not need extra wait time.
// @TODO Any filters?

type RateNode struct {
	Minimal   decimal.Decimal
	Median    decimal.Decimal
	CheckedAt time.Time
}

var rateMap = map[string]RateNode{}
var rateMapLock sync.RWMutex

func fetchRate(currency string) (RateNode, error) {
	ad, err := conf.LBKey.BuyOnlineList(currency)
	if err != nil {
		return RateNode{}, err
	}
	if len(ad) == 0 {
		return RateNode{}, errors.New("no offers available")
	}
	// Results should be sorted(i believe), so just take first and middle values
	return RateNode{
		Minimal:   ad[0].Data.TempPrice,
		Median:    ad[len(ad)/2].Data.TempPrice,
		CheckedAt: time.Now(),
	}, nil
}

func GetExchangeRate(currency string) (RateNode, error) {
	rateMapLock.RLock()
	node, ok := rateMap[currency]
	rateMapLock.RUnlock()

	if ok && time.Now().Sub(node.CheckedAt) < RateRefreshTimeout {
		return node, nil
	}

	node, err := fetchRate(currency)
	if err != nil {
		return node, err
	}

	rateMapLock.Lock()
	rateMap[currency] = node
	rateMapLock.Unlock()

	return node, nil
}
