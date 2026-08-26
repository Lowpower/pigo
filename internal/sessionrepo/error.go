package sessionrepo

import "fmt"

// ErrorCode is a durable session backend error class.
type ErrorCode string

// Session error codes.
const (
	ErrNotFound          ErrorCode = "not_found"
	ErrAlreadyExists     ErrorCode = "already_exists"
	ErrInvalidEntry      ErrorCode = "invalid_entry"
	ErrInvalidPayload    ErrorCode = "invalid_payload"
	ErrInvalidLane       ErrorCode = "invalid_lane"
	ErrInvalidQuery      ErrorCode = "invalid_query"
	ErrInvalidForkTarget ErrorCode = "invalid_fork_target"
	ErrStorage           ErrorCode = "storage"
)

// Error is a session backend failure with a stable code.
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s", e.Message, e.Cause.Error())
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsCode reports whether err is a sessionrepo.Error with the given code.
func IsCode(err error, code ErrorCode) bool {
	for err != nil {
		if as, ok := err.(*Error); ok {
			return as.Code == code
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			break
		}
		err = u.Unwrap()
	}
	return false
}

// NewError constructs a session error.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewErrorCause constructs a session error with a cause.
func NewErrorCause(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}
