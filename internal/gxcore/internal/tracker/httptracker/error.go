package httptracker

import (
	"net/http"
	"strconv"
)

// StatusError reports an HTTP tracker response whose status is not 200 OK.
// It carries the body so callers can surface the tracker's failure reason.
type StatusError struct {
	Code   int
	Header http.Header
	Body   string
}

func (e *StatusError) Error() string {
	return "http status: " + strconv.Itoa(e.Code)
}
