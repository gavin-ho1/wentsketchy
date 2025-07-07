package clock

import "time"

type Clock interface {
	Now() time.Time
}

const Date = "2006-01-02"
const DateTime = "2006-01-02 03:04:05 PM"
const Time = "03:04:05 PM"
const HoursMinutes = "3:04 PM"

type SystemCock struct{}

func NewSystemCock() Clock {
	return &SystemCock{}
}

func (r *SystemCock) Now() time.Time {
	return time.Now()
}
