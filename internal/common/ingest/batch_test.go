package ingest

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clock "k8s.io/utils/clock/testing"

	"github.com/armadaproject/armada/internal/common/armadacontext"
)

const (
	defaultMaxItems   = 3
	defaultMaxTimeOut = 5 * time.Second
)

var defaultItemCounterFunc = func(i int) int { return 1 }

type resultHolder struct {
	result [][]int
	mutex  sync.Mutex
}

func addToResult(result *resultHolder, publishChan chan []int) {
	for {
		select {
		case value, ok := <-publishChan:
			if !ok {
				return
			}
			result.add(value)
		}
	}
}

func newResultHolder() *resultHolder {
	return &resultHolder{
		result: make([][]int, 0),
		mutex:  sync.Mutex{},
	}
}

func (r *resultHolder) add(a []int) {
	r.mutex.Lock()
	r.result = append(r.result, a)
	r.mutex.Unlock()
}

func (r *resultHolder) resultLength() int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return len(r.result)
}

func TestBatch_MaxItems(t *testing.T) {
	ctx, cancel := armadacontext.WithTimeout(armadacontext.Background(), 5*time.Second)
	testClock := clock.NewFakeClock(time.Now())
	inputChan := make(chan int)
	publishChan := make(chan []int)
	result := newResultHolder()
	batcher := NewBatcher[int](inputChan, defaultMaxItems, defaultMaxTimeOut, defaultItemCounterFunc, publishChan)
	batcher.clock = testClock

	go addToResult(result, publishChan)

	go func() {
		batcher.Run(ctx)
		close(inputChan)
		close(publishChan)
	}()

	// Post 6 items on the input channel without advancing the clock
	// And we should get a 2 updates on the output channel
	inputChan <- 1
	inputChan <- 2
	inputChan <- 3
	inputChan <- 4
	inputChan <- 5
	inputChan <- 6
	waitForExpectedEvents(ctx, result, 2)
	assert.Equal(t, [][]int{{1, 2, 3}, {4, 5, 6}}, result.result)
	cancel()
}

func TestBatch_MaxItems_CustomItemCountFunction(t *testing.T) {
	ctx, cancel := armadacontext.WithTimeout(armadacontext.Background(), 5*time.Second)
	testClock := clock.NewFakeClock(time.Now())
	inputChan := make(chan int)
	publishChan := make(chan []int)
	result := newResultHolder()
	// This function will mean each item on the input channel will count as 2 items
	doubleItemCounterFunc := func(i int) int { return 2 }
	batcher := NewBatcher[int](inputChan, defaultMaxItems, defaultMaxTimeOut, doubleItemCounterFunc, publishChan)
	batcher.clock = testClock

	go addToResult(result, publishChan)

	go func() {
		batcher.Run(ctx)
		close(inputChan)
		close(publishChan)
	}()

	// Post 6 items on the input channel without advancing the clock
	// And we should get a 3 updates on the output channel
	inputChan <- 1
	inputChan <- 2
	inputChan <- 3
	inputChan <- 4
	inputChan <- 5
	inputChan <- 6
	waitForExpectedEvents(ctx, result, 3)
	assert.Equal(t, [][]int{{1, 2}, {3, 4}, {5, 6}}, result.result)
	cancel()
}

func TestBatch_Time(t *testing.T) {
	ctx, cancel := armadacontext.WithTimeout(armadacontext.Background(), 5*time.Second)
	testClock := clock.NewFakeClock(time.Now())
	inputChan := make(chan int)
	publishChan := make(chan []int)
	result := newResultHolder()
	batcher := NewBatcher[int](inputChan, defaultMaxItems, defaultMaxTimeOut, defaultItemCounterFunc, publishChan)
	batcher.clock = testClock

	go addToResult(result, publishChan)

	go func() {
		batcher.Run(ctx)
		close(inputChan)
		close(publishChan)
	}()

	inputChan <- 1
	inputChan <- 2
	err := waitForBufferLength(ctx, batcher, 2)
	require.NoError(t, err)
	testClock.Step(5 * time.Second)
	waitForExpectedEvents(ctx, result, 1)
	assert.Equal(t, [][]int{{1, 2}}, result.result)
	cancel()
}

func TestBatch_Time_WithIntialQuiet(t *testing.T) {
	ctx, cancel := armadacontext.WithTimeout(armadacontext.Background(), 5*time.Second)
	testClock := clock.NewFakeClock(time.Now())
	inputChan := make(chan int)
	publishChan := make(chan []int)
	result := newResultHolder()
	batcher := NewBatcher[int](inputChan, defaultMaxItems, defaultMaxTimeOut, defaultItemCounterFunc, publishChan)
	batcher.clock = testClock

	go addToResult(result, publishChan)

	go func() {
		batcher.Run(ctx)
		close(inputChan)
		close(publishChan)
	}()

	// initial quiet period
	testClock.Step(5 * time.Second)

	inputChan <- 1
	inputChan <- 2
	err := waitForBufferLength(ctx, batcher, 2)
	require.NoError(t, err)
	testClock.Step(5 * time.Second)
	waitForExpectedEvents(ctx, result, 1)
	inputChan <- 3
	inputChan <- 4
	err = waitForBufferLength(ctx, batcher, 2)
	require.NoError(t, err)

	testClock.Step(5 * time.Second)
	waitForExpectedEvents(ctx, result, 2)
	assert.Equal(t, [][]int{{1, 2}, {3, 4}}, result.result)
	cancel()
}

func waitForBufferLength(ctx *armadacontext.Context, batcher *Batcher[int], numEvents int) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if batcher.BufferLen() >= numEvents {
				return nil
			}
		}
	}
}

func waitForExpectedEvents(ctx *armadacontext.Context, rh *resultHolder, numEvents int) {
	done := false
	ticker := time.NewTicker(5 * time.Millisecond)
	for !done {
		select {
		case <-ctx.Done():
			done = true
		case <-ticker.C:
			if rh.resultLength() >= numEvents {
				done = true
			}
		}
	}
}
