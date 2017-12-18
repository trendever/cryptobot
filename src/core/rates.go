package main

import (
	"common/log"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"sync"
	"time"
)

const RateRefreshTickDefault = 5 * time.Minute

// @TODO Any filters?

type RateNode struct {
	Minimal   decimal.Decimal
	Median    decimal.Decimal
	CheckedAt time.Time
}

var (
	// List of currencies to refresh
	activeList  []string
	rateMap     = map[string]RateNode{}
	rateMapLock sync.RWMutex
)

func RatesRefresh(prefetch []string) {
	activeList = prefetch

	var tick time.Duration = RateRefreshTickDefault
	if conf.RatesRefreshTick != "" {
		parsed, err := time.ParseDuration(conf.RatesRefreshTick)
		if err != nil || parsed < time.Second {
			log.Errorf("invalid RateRefreshTick '%v'", conf.RatesRefreshTick)
		} else {
			tick = parsed
		}
	}

	fetchAll()

	for range time.Tick(tick) {
		fetchAll()
	}
}

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

func fetchAll() {
	for _, currency := range activeList {
		node, err := fetchRate(currency)
		if err != nil {
			log.Errorf("failed to update rate for currency %v: %v", currency, err)
		}
		rateMapLock.Lock()
		rateMap[currency] = node
		rateMapLock.Unlock()
	}
}

func GetExchangeRate(currency string) (RateNode, error) {
	rateMapLock.RLock()
	node, ok := rateMap[currency]
	rateMapLock.RUnlock()

	if ok {
		return node, nil
	}

	node, err := fetchRate(currency)
	if err != nil {
		return node, err
	}

	rateMapLock.Lock()
	rateMap[currency] = node
	activeList = append(activeList, currency)
	rateMapLock.Unlock()

	return node, nil
}
