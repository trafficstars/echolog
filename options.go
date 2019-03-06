package echolog

import (
	"github.com/sirupsen/logrus"
)

type Options struct {
	DebugLogLevelFraction    float32 // A fraction of traffic (requests) that should be logged on all levels (trace, debug, info, ...), not only error and higher
	EnableStackTraceFraction float32 // A fraction of requests, which will be logged with attached stack traces.
	Logger                   logrus.FieldLogger
}
