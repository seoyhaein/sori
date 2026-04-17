package sori

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	KindValidation ErrorKind = "validation"
	KindNotFound   ErrorKind = "not_found"
	KindConflict   ErrorKind = "conflict"
	KindIntegrity  ErrorKind = "integrity"
	KindTransport  ErrorKind = "transport"
	KindAuth       ErrorKind = "auth"
)

type Error struct {
	Kind    ErrorKind
	Op      string
	Message string
	Err     error
}

func (e *Error) Error() string {
	switch {
	case e == nil:
		return "<nil>"
	case e.Message != "" && e.Err != nil:
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	case e.Message != "":
		return fmt.Sprintf("%s: %s", e.Op, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	default:
		return e.Op
	}
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	if t.Kind != "" && e.Kind != t.Kind {
		return false
	}
	if t.Op != "" && e.Op != t.Op {
		return false
	}
	return true
}

var (
	ErrValidation = &Error{Kind: KindValidation}
	ErrNotFound   = &Error{Kind: KindNotFound}
	ErrConflict   = &Error{Kind: KindConflict}
	ErrIntegrity  = &Error{Kind: KindIntegrity}
	ErrTransport  = &Error{Kind: KindTransport}
	ErrAuth       = &Error{Kind: KindAuth}
)

func newError(kind ErrorKind, op, message string, err error) error {
	return &Error{Kind: kind, Op: op, Message: message, Err: err}
}

func validationError(op, message string, err error) error {
	return newError(KindValidation, op, message, err)
}

func notFoundError(op, message string, err error) error {
	return newError(KindNotFound, op, message, err)
}

func conflictError(op, message string, err error) error {
	return newError(KindConflict, op, message, err)
}

func integrityError(op, message string, err error) error {
	return newError(KindIntegrity, op, message, err)
}

func transportError(op, message string, err error) error {
	return newError(KindTransport, op, message, err)
}

func authError(op, message string, err error) error {
	return newError(KindAuth, op, message, err)
}

func isKind(err error, target error) bool {
	return errors.Is(err, target)
}
