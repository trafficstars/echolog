package echolog

import (
	"github.com/labstack/echo"
)

func Middleware(opts Options) echo.MiddlewareFunc {
	loggerContextGenerator := newLoggerContextGenerator(opts)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(echoContext echo.Context) (err error) {
			// Get a context with an embedded logger
			c := loggerContextGenerator.AcquireContext(echoContext)

			// OK, now we call the real request handler
			// This handler can call logger's methods from the context
			err = next(c)

			// Release the context to reuse it in future (this way is faster than always generate a new object and throw it to the GC)
			c.Release()
			return
		}
	}
}
