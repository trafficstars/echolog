package echolog

import (
	labstacklog "github.com/labstack/gommon/log"
)

type defaultContextLoggerSettingsT struct {
	defaultLogLevel          labstacklog.Lvl
	debugLogLevelFraction    float32
	enableStackTraceFraction float32
}

var defaultContextLoggerSettings = defaultContextLoggerSettingsT{
	defaultLogLevel: labstacklog.ERROR,
}
