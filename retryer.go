package main

import "time"

func ReqWithRetries[T any](f func() (T, error)) (T, error) {
	var err error
	var res T
	for i := 0; i < 5; i++ {
		res, err = f()
		if err == nil {
			return res, nil
		}
		time.Sleep(time.Millisecond * 500 * time.Duration(i))
	}

	return res, err
}
