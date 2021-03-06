package echolog

import (
	"io"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"

	labstacklog "github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
	"github.com/trafficstars/echo"
	echolog "github.com/trafficstars/echo/log"
)

// CtxShouldLogExchange is used as context key to enforce request/response logging
const CtxShouldLogExchange = "ctx_write_exchange_log"

type echoContext = echo.Context

type LoggerContextLogger struct {
	requestID           string
	logger              logrus.FieldLogger
	LogLevel            labstacklog.Lvl
	IsStackTraceEnabled bool
	StartTime           time.Time
	cache               *cache
}
type contextLogger = LoggerContextLogger // To be able to do that as a private anonymous variable
type ContextLogger = LoggerContextLogger // Just a shortcut

type LoggerContext struct {
	echoContext
	contextLogger

	generator *loggerContextGenerator
}

var (
	startTime time.Time
)

func init() {
	startTime = time.Now()
}

func (ctx *LoggerContext) init(
	generator *loggerContextGenerator,
	origCtx echo.Context,
	requestID string,
	logger logrus.FieldLogger,
	logLevel labstacklog.Lvl,
	isStackTraceEnabled bool,
	isCachingEnabled bool,
	startTime time.Time,
) {
	ctx.generator = generator
	ctx.echoContext = origCtx
	ctx.requestID = requestID
	ctx.logger = logger.WithField(`request_id`, requestID)
	ctx.LogLevel = logLevel
	ctx.IsStackTraceEnabled = isStackTraceEnabled
	ctx.StartTime = startTime
	if isCachingEnabled {
		ctx.cache = &cache{data: make([]string, 0, 0)}
	}
}

func GetDefaultContextLogger() *LoggerContextLogger {
	r := &LoggerContextLogger{
		requestID:           `undefined`,
		logger:              GetDefaultLogger(),
		LogLevel:            defaultContextLoggerSettings.defaultLogLevel,
		IsStackTraceEnabled: rand.Float32() < defaultContextLoggerSettings.enableStackTraceFraction,
	}

	if rand.Float32() < defaultContextLoggerSettings.debugLogLevelFraction {
		r.LogLevel = labstacklog.DEBUG
	}

	return r
}

func (ctx *LoggerContext) SetLevel(newLogLevel labstacklog.Lvl) {
	ctx.LogLevel = newLogLevel
}

func (ctx *LoggerContext) Get(key string) interface{} {
	switch key {
	case `request_id`:
		return ctx.GetRequestID()
	case `log_level`:
		return ctx.LogLevel
	case `is_backtracing_enabled`:
		return ctx.IsStackTraceEnabled
	case `logger`:
		return ctx
	}

	return ctx.echoContext.Get(key)
}

func (ctx *LoggerContext) Set(key string, value interface{}) {
	switch key {
	case `request_id`, `logger`:
		ctx.contextLogger.Error(`Context fields "request_id" and "logger" are read-only`)
		return
	case `log_level`:
		newLogLevel, ok := value.(labstacklog.Lvl)
		if !ok {
			ctx.Errorf(`Cannot set to "log_level" a value of type "%T" (required github.com/labstack/gommon/log.Lvl)`, value)
			return
		}
		ctx.LogLevel = newLogLevel
	case `is_backtracing_enabled`:
		newIsStackTraceEnabled, ok := value.(bool)
		if !ok {
			ctx.Errorf(`Cannot set to "is_backtracing_enabled" a non-bool value: %T`, value)
			return
		}
		ctx.IsStackTraceEnabled = newIsStackTraceEnabled
		return
	}

	ctx.echoContext.Set(key, value)
}

func (ctx *LoggerContext) Error(err error) {
	ctx.contextLogger.Error(err)
}

func (ctx *LoggerContext) Logger() echolog.Logger {
	return &ctx.contextLogger
}

func (ctx *LoggerContext) GetRequestID() string {
	return ctx.requestID
}

func (ctx *LoggerContext) GetLogLevel() labstacklog.Lvl {
	return ctx.LogLevel
}

// IfShouldWriteExchangeLog - executes function if we should
// write request/response log during this context
func (ctx *LoggerContext) IfShouldWriteExchangeLog(fn func()) {

	// value in context have highest priority
	var shouldWrite bool
	ctxValue := ctx.Get(CtxShouldLogExchange)
	if ctxValue != nil {
		if shouldWrite, ok := ctxValue.(bool); ok && !shouldWrite {
			return
		}
	}

	if shouldWrite || ctx.LogLevel == labstacklog.DEBUG {
		fn()
	}
}

func (ctxLogger *LoggerContextLogger) IfDebug(fn func()) {
	if ctxLogger.LogLevel > labstacklog.DEBUG {
		return
	}
	fn()
}

func (ctxLogger *LoggerContextLogger) SetLevel(newLogLevel labstacklog.Lvl) {
	ctxLogger.LogLevel = newLogLevel
}

func (ctxLogger LoggerContextLogger) ScopeEnableStackTrace(newEnableStackTrace bool) *LoggerContextLogger {
	ctxLogger.IsStackTraceEnabled = newEnableStackTrace
	return &ctxLogger
}

func (ctxLogger LoggerContextLogger) WithField(key string, value interface{}) *LoggerContextLogger {
	ctxLogger.logger = ctxLogger.logger.WithField(key, value)
	return &ctxLogger
}

type Fields = logrus.Fields

// WithFields create a new scope with the fields
func (ctxLogger LoggerContextLogger) WithFields(fields logrus.Fields) *LoggerContextLogger {
	// ctxLogger is not a pointer, so it's a copy here:
	return ctxLogger.SetFields(fields)
}

// SetFields sets the fields within the current scope
func (ctxLogger *LoggerContextLogger) SetFields(fields logrus.Fields) *LoggerContextLogger {
	ctxLogger.logger = ctxLogger.logger.WithFields(fields)
	return ctxLogger
}

func (ctxLogger *LoggerContextLogger) getPreparedLogger() logrus.FieldLogger {
	// TODO: this's quite slow and stupid method. Fix the performance issue.

	logger := ctxLogger.logger
	stack := string(debug.Stack())

	stackLines := strings.Split(stack, "\n")
	for _, line := range stackLines[3:] {
		if line[0] != '\t' {
			continue
		}
		if strings.Index(line, `echolog`) == -1 {
			line = line[1:]
			logger = logger.WithField(`line`, line)
			break
		}
	}

	if ctxLogger.IsStackTraceEnabled {
		logger = logger.WithField(`stack_trace`, stack)
	}
	if !ctxLogger.StartTime.IsZero() {
		logger = logger.WithField(`request_time`, time.Since(ctxLogger.StartTime))
	}
	logger = logger.WithField(`uptime`, time.Since(startTime))

	// Remember which log level was used by environment when we sent it
	// Useful to find requests which enforced debug level
	logger = logger.WithField(`ctx_logger_level`, ctxLogger.LogLevel)

	// TODO: remove this hack:
	atomic.StoreUint32((*uint32)(&logger.(*logrus.Entry).Level), uint32(logrus.TraceLevel))

	return logger
}

func (ctxLogger *LoggerContextLogger) Debugf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.DEBUG {
		return
	}
	ctxLogger.getPreparedLogger().Debugf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Infof(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Infof(format, args...)
}
func (ctxLogger *LoggerContextLogger) Printf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Printf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Warnf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warnf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Warningf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warningf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Errorf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	if ctxLogger.LogLevel > labstacklog.ERROR {
		return
	}
	ctxLogger.getPreparedLogger().Errorf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Fatalf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Fatalf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Panicf(format string, args ...interface{}) {
	ctxLogger.cache.Putf(ctxLogger.LogLevel, format, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Panicf(format, args...)
}
func (ctxLogger *LoggerContextLogger) Debug(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.DEBUG {
		return
	}
	ctxLogger.getPreparedLogger().Debug(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Info(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Info(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Print(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Print(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Warn(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warn(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Warning(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warning(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Error(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.ERROR {
		return
	}
	ctxLogger.getPreparedLogger().Error(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Fatal(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Fatal(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Panic(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Panic(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Debugln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.DEBUG {
		return
	}
	ctxLogger.getPreparedLogger().Debugln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Infoln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Infoln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Println(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().Println(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Warnln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warnln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Warningln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().Warningln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Errorln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	if ctxLogger.LogLevel > labstacklog.ERROR {
		return
	}
	ctxLogger.getPreparedLogger().Errorln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Fatalln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Fatalln(addSpacesToArgs(args)...)
}
func (ctxLogger *LoggerContextLogger) Panicln(args ...interface{}) {
	ctxLogger.cache.Put(ctxLogger.LogLevel, args...)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().Panicln(addSpacesToArgs(args)...)
}

func (ctxLogger *LoggerContextLogger) Debugj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.DEBUG {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Debug(`d`)
}
func (ctxLogger *LoggerContextLogger) Infoj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Info(`i`)
}
func (ctxLogger *LoggerContextLogger) Printj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.INFO {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Print(`p`)
}
func (ctxLogger *LoggerContextLogger) Warnj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Warn(`w`)
}
func (ctxLogger *LoggerContextLogger) Warningj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.WARN {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Warning(`w`)
}
func (ctxLogger *LoggerContextLogger) Errorj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	if ctxLogger.LogLevel > labstacklog.ERROR {
		return
	}
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Error(`e`)
}
func (ctxLogger *LoggerContextLogger) Fatalj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Fatal(`f`)
}
func (ctxLogger *LoggerContextLogger) Panicj(j labstacklog.JSON) {
	ctxLogger.cache.Putj(ctxLogger.LogLevel, j)
	ctxLogger.IsStackTraceEnabled = true
	ctxLogger.getPreparedLogger().WithFields(logrus.Fields(j)).Panic(`p`)
}
func (ctxLogger *LoggerContextLogger) SetOutput(w io.Writer) {
	ctxLogger.ScopeEnableStackTrace(true).Warning(`Changing output of the logger`)
	switch l := ctxLogger.logger.(type) {
	case *logrus.Entry:
		l.Logger.SetOutput(w)
	case *logrus.Logger:
		l.SetOutput(w)
	default:
		ctxLogger.Errorf(`Don't know how to set an output of logger of type "%T"`, l)
	}
}

func (ctxLogger *LoggerContextLogger) Cache() []string {
	return ctxLogger.cache.Retrieve()
}

func (ctx *LoggerContext) Release() {
	if ctx.generator == nil {
		return
	}
	ctx.generator.releaseContext(ctx)
}

func addSpacesToArgs(args []interface{}) []interface{} {
	if len(args) == 0 {
		return args
	}
	r := make([]interface{}, 0, len(args)*2-1)
	r = append(r, args[0])
	for _, arg := range args[1:] {
		r = append(r, ` `)
		r = append(r, arg)
	}

	return r
}
