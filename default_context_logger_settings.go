package echolog

import (
	labstacklog "github.com/labstack/gommon/log"
)

const DefaultContextLoggerLevel = labstacklog.ERROR

type defaultContextLoggerSettingsT struct {
	defaultLogLevel          labstacklog.Lvl
	debugLogLevelFraction    float32
	enableStackTraceFraction float32
}

var defaultContextLoggerSettings = defaultContextLoggerSettingsT{
	defaultLogLevel: DefaultContextLoggerLevel,
}
