// Package core errors.
package core

import "fmt"

// ErrorCode identifies the kind of failure.
type ErrorCode string

const (
	CodeConfigInvalid       ErrorCode = "CONFIG_INVALID"
	CodeAuthRequired        ErrorCode = "AUTH_REQUIRED"
	CodeAuthFailed          ErrorCode = "AUTH_FAILED"
	CodeCaptchaRequired     ErrorCode = "CAPTCHA_REQUIRED"
	CodeHTTPError           ErrorCode = "HTTP_ERROR"
	CodeAPIError            ErrorCode = "API_ERROR"
	CodeAmountPoolExhausted ErrorCode = "AMOUNT_POOL_EXHAUSTED"
	CodeQRISParseError      ErrorCode = "QRIS_PARSE_ERROR"
)

// BaseError is the base error type for all public failures. Every error
// surfaced to callers is a *BaseError (or a subtype), keeping error handling
// predictable. The name is BaseError rather than Error so that embedding it
// does not shadow the Error() method.
type BaseError struct {
	Code    ErrorCode
	Message string
	Cause   error
	Details map[string]any
}

func (e *BaseError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *BaseError) Unwrap() error { return e.Cause }

// --- Constructors ---

// NewBaseError creates a new BaseError with the given code and message.
func NewBaseError(code ErrorCode, msg string) *BaseError {
	return &BaseError{Code: code, Message: msg}
}

// NewBaseErrorf creates a new BaseError with a formatted message.
func NewBaseErrorf(code ErrorCode, format string, args ...any) *BaseError {
	return &BaseError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WrapBaseError wraps an existing error with a code and message.
func WrapBaseError(code ErrorCode, msg string, cause error) *BaseError {
	return &BaseError{Code: code, Message: msg, Cause: cause}
}

// --- Subtypes ---

// ConfigError is a configuration error.
type ConfigError struct {
	*BaseError
	Details map[string]any
}

// NewConfigError creates a ConfigError.
func NewConfigError(msg string, details map[string]any) *ConfigError {
	return &ConfigError{
		BaseError: NewBaseError(CodeConfigInvalid, msg),
		Details:   details,
	}
}

// AuthError is an authentication error (AUTH_REQUIRED or AUTH_FAILED).
type AuthError struct {
	*BaseError
}

// NewAuthError creates an AuthError.
func NewAuthError(code ErrorCode, msg string, cause error) *AuthError {
	return &AuthError{BaseError: WrapBaseError(code, msg, cause)}
}

// CaptchaRequiredError signals that authentication cannot continue until the
// caller completes a CAPTCHA.
type CaptchaRequiredError struct {
	*BaseError
}

// NewCaptchaRequiredError creates a CaptchaRequiredError.
func NewCaptchaRequiredError(msg string) *CaptchaRequiredError {
	if msg == "" {
		msg = "CAPTCHA is required to continue authentication"
	}
	return &CaptchaRequiredError{BaseError: NewBaseError(CodeCaptchaRequired, msg)}
}

// HTTPError is an HTTP-level error with a status code.
type HTTPError struct {
	*BaseError
	Status int
	Body   any
}

// NewHTTPError creates an HTTPError.
func NewHTTPError(status int, msg string, body any) *HTTPError {
	return &HTTPError{
		BaseError: NewBaseError(CodeHTTPError, msg),
		Status:    status,
		Body:      body,
	}
}

// APIError is an API-level error with an optional api code.
type APIError struct {
	*BaseError
	APICode string
}

// NewAPIError creates an APIError.
func NewAPIError(msg string, apiCode string, cause error) *APIError {
	return &APIError{
		BaseError: WrapBaseError(CodeAPIError, msg, cause),
		APICode:   apiCode,
	}
}

// IsErrorCode checks whether err (or any error in its chain) is a *BaseError
// with the given code.
func IsErrorCode(err error, code ErrorCode) bool {
	var e *BaseError
	if AsBaseError(err, &e) {
		return e.Code == code
	}
	return false
}

// AsBaseError extracts the first *BaseError in the error chain. Subtypes
// (AuthError, APIError, …) embed *BaseError rather than being one, so each
// concrete subtype is resolved explicitly before following the Unwrap chain.
func AsBaseError(err error, target **BaseError) bool {
	for err != nil {
		switch e := err.(type) {
		case *BaseError:
			*target = e
			return true
		case *AuthError:
			*target = e.BaseError
			return true
		case *APIError:
			*target = e.BaseError
			return true
		case *HTTPError:
			*target = e.BaseError
			return true
		case *ConfigError:
			*target = e.BaseError
			return true
		case *CaptchaRequiredError:
			*target = e.BaseError
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
