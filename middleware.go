package echolog

import (
	"bytes"

	labstacklog "github.com/labstack/gommon/log"
	"github.com/sirupsen/logrus"
	"github.com/trafficstars/echo"
	"github.com/trafficstars/echo/engine"
	"github.com/trafficstars/echo/engine/fasthttp"
)

// getHeaders extracts HTTP headers from an engine.Request
func getHeaders(headerObj engine.Header) map[string]string {
	headers := map[string]string{}
	for _, k := range headerObj.Keys() {
		headers[k] = headerObj.Get(k)
	}

	return headers
}

// getBodyAndHeadersFromRequest extracts HTTP request body and headers from an engine.Request
func getBodyAndHeadersFromRequest(req engine.Request) (body string, headers map[string]string) {
	var bodyBuf bytes.Buffer
	bodyBuf.ReadFrom(req.Body())

	return bodyBuf.String(), getHeaders(req.Header())
}

// Middleware is the function to be used as an argument to method `Use()` of an echo router
//
// This's the function that should be used from external packages.
func Middleware(opts Options) echo.MiddlewareFunc {
	loggerContextGenerator := newLoggerContextGenerator(opts)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(echoContext echo.Context) (err error) {

			// Get a context with an embedded logger
			c := loggerContextGenerator.AcquireContext(echoContext)

			// OK, now we call the real request handler
			// This handler can call logger's methods from the context
			err = next(c)

			// We call IfShouldWriteLog to do not process unnecessary
			// heavy routines if it's not the case when we will really log it
			if !opts.IsRTB {
				c.IfShouldWriteExchangeLog(func() {

					// Check for logger level, it should be debug to execute
					currentLogLevel := c.LogLevel
					if c.LogLevel > labstacklog.DEBUG {
						c.LogLevel = labstacklog.DEBUG
					}
					// Restore logger level to initial value
					if c.LogLevel != currentLogLevel {
						defer func() { c.LogLevel = currentLogLevel }()
					}

					// Retrieve response body from fasthttp response object
					var responseBody string
					val, ok := c.Response().(*fasthttp.Response)
					if ok {
						responseBody = string(val.RequestCtx.Response.Body())
					}

					// Collect request body and headers
					body, headers := getBodyAndHeadersFromRequest(echoContext.Request())

					// Log request
					c.WithFields(logrus.Fields{
						`what`:         `http_request`,
						`method`:       echoContext.Request().Method(),
						`url`:          echoContext.Request().URL().Path(),
						`query_params`: echoContext.Request().URL().QueryString(),
						`http_headers`: headers,
					}).Debug(body)

					// Log Response
					c.WithFields(logrus.Fields{
						`what`:         `http_response`,
						`method`:       echoContext.Request().Method(),
						`url`:          echoContext.Request().URL().Path(),
						`query_params`: echoContext.Request().URL().QueryString(),
						`http_headers`: getHeaders(echoContext.Response().Header()),
						`http_code`:    echoContext.Response().Status(),
					}).Debug(responseBody)
				})
			}

			c.Response().Header().Set(`X-Request-Id`, c.GetRequestID())

			// Release the context to reuse it in future
			// (this way is faster than always generate a new object and throw it to the GC)
			c.Release()
			return
		}
	}
}
