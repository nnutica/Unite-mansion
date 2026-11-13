package httpx

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// Hooks for the oapi-codegen generated server. They hand every error to the
// Errors middleware instead of letting generated code write its own body.

// HandlerError forwards an error returned by a handler.
func HandlerError(c *gin.Context, err error) {
	c.Error(err)
}

// RequestError forwards a request parse/bind failure as a 400.
func RequestError(c *gin.Context, err error) {
	c.Error(fmt.Errorf("%w: %v", ErrBadRequest, err))
}

// ParamError adapts RequestError to GinServerOptions.ErrorHandler.
func ParamError(c *gin.Context, err error, _ int) {
	RequestError(c, err)
}
