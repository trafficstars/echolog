package echolog

import (
	"bytes"
	"io"

	"github.com/labstack/echo"
	"github.com/labstack/echo/engine"
	"github.com/sirupsen/logrus"
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

			// Log the request and prepare response logging
			var responseBodyCopy *bytes.Buffer
			c.IfDebug(func() {
				// We call IfDebug to do not process unnecessary heavy routines if it's not the case when we will really log it

				body, headers := getBodyAndHeadersFromRequest(echoContext.Request())

				c.WithFields(logrus.Fields{
					`method`:       echoContext.Request().Method(),
					`url`:          echoContext.Request().URL().Path(),
					`query_params`: echoContext.Request().URL().QueryParams(),
					`what`:         `http_request`,
					`http_headers`: headers,
				}).Debug(body)

				responseBodyCopy = &bytes.Buffer{}
				c.Response().SetWriter(io.MultiWriter(c.Response().Writer(), responseBodyCopy))
			})

			// OK, now we call the real request handler
			// This handler can call logger's methods from the context
			err = next(c)

			// Log the response
			c.IfDebug(func() {
				// We call IfDebug to do not process unnecessary heavy routines if it's not the case when we will really log it

				c.WithFields(logrus.Fields{
					`method`:       echoContext.Request().Method(),
					`url`:          echoContext.Request().URL().Path(),
					`query_params`: echoContext.Request().URL().QueryParams(),
					`what`:         `http_response`,
					`http_headers`: getHeaders(echoContext.Response().Header()),
					`http_code`:    echoContext.Response().Status(),
				}).Debug(responseBodyCopy.String())
			})

			c.Response().Header().Set(`X-Request-Id`, c.GetRequestID())

			// Release the context to reuse it in future (this way is faster than always generate a new object and throw it to the GC)
			c.Release()
			return
		}
	}
}
