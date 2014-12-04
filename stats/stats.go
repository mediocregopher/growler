package stats

import (
	"log"
	"time"
)

var getCh = make(chan int)
var headCh = make(chan int)
var totalCh = make(chan int)

func init() {
	go spin()
}

func spin() {
	getM := map[int]uint64{}
	headM := map[int]uint64{}
	totalM := map[int]uint64{}
	tick := time.Tick(5 * time.Second)
	for {
		select {
			case i := <-getCh:
				getM[i]++
			case i := <-headCh:
				headM[i]++
			case i := <-totalCh:
				totalM[i]++
			case <-tick:
				log.Printf("STAT output:")
				for i := range totalM {
					log.Printf(
						"STAT: %d total:%d gets:%d heads:%d",
						i,
						totalM[i],
						getM[i],
						headM[i],
					)
				}
		}
	}
}

func IncrGet(i int) {
	getCh <- i
}

func IncrHead(i int) {
	headCh <- i
}

func IncrTotal(i int) {
	totalCh <- i
}
