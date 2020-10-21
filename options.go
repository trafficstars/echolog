package echolog

import (
	labstacklog "github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
)

type Options struct {
	Disable                  bool    // Disable request / response logging
	CacheLogs                bool    // Save session logs in buffer, can be retrieved with .Cache()
	DebugLogLevelFraction    float32 // A fraction of traffic that should be logged on all levels
	EnableStackTraceFraction float32 // A fraction of requests, which will be logged with attached stack traces.
	DefaultLogLevel          labstacklog.Lvl
	Logger                   logrus.FieldLogger
}
