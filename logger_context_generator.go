package echolog

import (
	"encoding/hex"
	"math/rand"
	"strings"
	"sync"

	"github.com/labstack/echo"
	labstacklog "github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
)

const (
	randomRequestIDLen = 16 // If it's unable to find a request ID in headers/GET-parameters then we generate a random ID. This's the length of the requestID in such case.
)

type loggerContextGenerator struct {
	debugLogLevelFraction          float32
	enableStackTraceFraction       float32
	contextPool                    sync.Pool // a pool of *loggerContext
	generateRequestIDReusablesPool sync.Pool // a pool of *generateRequestIDReusables
	defaultLogger                  logrus.FieldLogger
	defaultLogLevel                labstacklog.Lvl
}

// The registry of all "loggerContextGenerator"'s.
type loggerContextGeneratorsT struct {
	sync.Mutex
	slice []*loggerContextGenerator
}

var loggerContextGenerators = loggerContextGeneratorsT{}

func newDefaultLogger() logrus.FieldLogger {
	return logrus.StandardLogger()
}

func newLoggerContextGenerator(opts Options) *loggerContextGenerator {
	logger := opts.Logger
	if logger == nil {
		logger = newDefaultLogger()
	}

	gen := &loggerContextGenerator{
		contextPool: sync.Pool{
			New: func() interface{} {
				return &LoggerContext{}
			},
		},
		generateRequestIDReusablesPool: sync.Pool{
			New: func() interface{} {
				return &generateRequestIDReusables{}
			},
		},
		debugLogLevelFraction:    opts.DebugLogLevelFraction,
		enableStackTraceFraction: opts.EnableStackTraceFraction,
		defaultLogLevel:          opts.DefaultLogLevel,
		defaultLogger:            logger,
	}

	loggerContextGenerators.Lock()
	loggerContextGenerators.slice = append(loggerContextGenerators.slice, gen)
	loggerContextGenerators.Unlock()

	return gen
}

func (h *loggerContextGenerator) SetDebugLogLevelFraction(newDebugLogLevelFraction float32) {
	// We don't do any atomicity here because it's not important. This method is supposed to be called rarely.
	h.debugLogLevelFraction = newDebugLogLevelFraction
}

func SetDebugLogLevelFraction(newExtraLoggingFraction float32) {
	// The lock is prevent panics caused by modification of length "loggerContextGenerators.slice" from another goroutine
	loggerContextGenerators.Lock()
	for _, gen := range loggerContextGenerators.slice {
		gen.SetDebugLogLevelFraction(newExtraLoggingFraction)
	}
	loggerContextGenerators.Unlock()
}

func (h *loggerContextGenerator) SetEnableStackTraceFraction(newEnableStackTraceFraction float32) {
	// We don't do any atomicity here because it's not important. This method is supposed to be called rarely.
	h.enableStackTraceFraction = newEnableStackTraceFraction
}

func SetEnableStackTraceFraction(newStackTraceFraction float32) {
	// The lock is prevent panics caused by modification of length "loggerContextGenerators.slice" from another goroutine
	loggerContextGenerators.Lock()
	for _, gen := range loggerContextGenerators.slice {
		gen.SetEnableStackTraceFraction(newStackTraceFraction)
	}
	loggerContextGenerators.Unlock()
}

func (h *loggerContextGenerator) SetDefaultLogLevel(newDefaultLogLevel labstacklog.Lvl) {
	// We don't do any atomicity here because it's not important. This method is supposed to be called rarely.
	h.defaultLogLevel = newDefaultLogLevel
}

func SetDefaultLogLevel(newDefaultLogLevel labstacklog.Lvl) {
	// The lock is prevent panics caused by modification of length "loggerContextGenerators.slice" from another goroutine
	loggerContextGenerators.Lock()
	for _, gen := range loggerContextGenerators.slice {
		gen.SetDefaultLogLevel(newDefaultLogLevel)
	}
	loggerContextGenerators.Unlock()
}

type generateRequestIDReusables struct {
	randomBuffer [(randomRequestIDLen + 1) / 2]byte
	hexBuffer    [randomRequestIDLen]byte
}

func (h *loggerContextGenerator) generateRandomRequestID() string {
	reusables := h.generateRequestIDReusablesPool.Get().(*generateRequestIDReusables)

	_, err := rand.Read(reusables.randomBuffer[:])
	if err != nil {
		// TODO: additionally send an error through the logger
		h.generateRequestIDReusablesPool.Put(reusables)
		return `cannot_generate_case0: ` + err.Error()
	}
	hex.Encode(reusables.hexBuffer[:], reusables.randomBuffer[:])

	r := string(reusables.hexBuffer[:])

	h.generateRequestIDReusablesPool.Put(reusables)
	return r
}

func (h *loggerContextGenerator) getRequestID(c echo.Context) string {
	header := c.Request().Header()
	params := c.QueryParams()

	var requestID string
	if len(params[`x_log_request_id`]) > 0 {
		requestID = params[`x_log_request_id`][0] // A request ID that we can manually pass through GET parameters if required
	}
	if requestID == `` {
		requestID = header.Get(`X-Log-Request-Id`) // A request ID that we can manually pass through headers if required
	}
	if requestID == `` {
		requestID = header.Get(`CF-RAY`) // CloudFlare's request ID
	}
	if requestID == `` {
		requestID = h.generateRandomRequestID()
	}

	return requestID
}

func TryParseLogLevel(s string, defaultLogLevel labstacklog.Lvl) labstacklog.Lvl {
	if len(s) == 0 {
		return defaultLogLevel
	}
	switch strings.ToLower(s) {
	case `d`, `debug`:
		return labstacklog.DEBUG
	case `i`, `info`:
		return labstacklog.INFO
	case `w`, `warn`, `warning`:
		return labstacklog.WARN
	case `e`, `err`, `error`:
		return labstacklog.ERROR
	case `o`, `off`:
		return labstacklog.OFF
	}
	return defaultLogLevel
}

func (h *loggerContextGenerator) AcquireContext(c echo.Context) *LoggerContext {
	// Will we send all the information related to the request?
	// We will send it if any of this conditions are satisfied:
	// * rand.Float64() < h.debugLogLevelFraction
	// * There's a HTTP header (in the request): X-Log-Extra: true
	// * There's a HTTP header (in the request): X-Log-Level: debug
	// * There's a query (GET) parameter "x_log_extra=true"
	// * There's a query (GET) parameter "x_log_level=debug"

	header := c.Request().Header()
	params := c.QueryParams()

	isDebugLogLevelEnabled := rand.Float32() < h.debugLogLevelFraction ||
		header.Get(`X-Log-Extra`) == `true`

	if !isDebugLogLevelEnabled {
		for _, v := range params[`x_log_extra`] {
			isDebugLogLevelEnabled = isDebugLogLevelEnabled || v == `true`
		}
	}

	logLevel := h.defaultLogLevel
	if isDebugLogLevelEnabled {
		logLevel = labstacklog.DEBUG
	}

	// Modify log level using a HTTP header if it contains a correct value
	logLevel = TryParseLogLevel(header.Get(`X-Log-Level`), logLevel)

	// Modify log level using a GET parameter if it contains a correct value
	for _, v := range params[`x_log_level`] {
		logLevel = TryParseLogLevel(v, logLevel)
	}

	// Will we send stackTraces (related to the request)?

	isStackTraceEnabled := rand.Float32() < h.enableStackTraceFraction ||
		header.Get(`X-Log-Stack-Traces`) == `true`

	if !isStackTraceEnabled {
		for _, v := range params[`x_log_stack_traces`] {
			isStackTraceEnabled = isStackTraceEnabled || v == `true`
		}
	}

	// The loggable context

	newContext := h.contextPool.Get().(*LoggerContext)
	newContext.init(h,
		c,
		h.getRequestID(c),
		h.defaultLogger,
		logLevel,
		isStackTraceEnabled,
	)

	return newContext
}

func (h *loggerContextGenerator) releaseContext(ctx echo.Context) {
	h.contextPool.Put(ctx)
}
