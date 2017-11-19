package main

type orderManager struct {
	waiters   []Order
	orders    chan Order
	operators chan Operator
}

var manager = orderManager{
	waiters:   make([]Order, 0),
	orders:    make(chan Order),
	operators: make(chan Operator),
}

func StartOrderManager() {
	go manager.loop()
}

func (man *orderManager) loop() {
	for {
		select {
		case order := <-man.orders:

		case op := <-man.operators:
			for _, order := range man.waiters {
				if op.Deposit.Cmp(order.LBAmount) > 0 {

				}
			}
		}
	}
}
