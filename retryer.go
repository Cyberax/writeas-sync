package main

import (
	"log/slog"
	"time"
)

func ReqWithRetries[T any](f func() (T, error)) (T, error) {
	time.Sleep(1 * time.Second)

	var err error
	var res T
	for i := 0; i < 3; i++ {
		res, err = f()
		if err == nil {
			return res, nil
		}

		timeToWaitMs := int64(15000 * (i + 2))
		slog.Default().Warn("Encountered an error. Deploying a workaround and waiting.",
			slog.Int64("time-to-wait-millis", timeToWaitMs))
		time.Sleep(time.Millisecond * time.Duration(timeToWaitMs))
	}

	return res, err
}
