// Custom error type storing a list of error strings.

package main

import "fmt"

type ErrorSeverity int

const (
	ESNone ErrorSeverity = iota
	ESDebug
	ESWarning
	ESError
	ESFatal
)

func (sev ErrorSeverity) String() string {
	switch sev {
	case ESDebug:
		return "**Debug** "
	case ESWarning:
		return "*Warning* "
	case ESError:
		return "**Error** "
	case ESFatal:
		return "**Fatal** "
	}
	return ""
}

type Error struct {
	s   string
	pos *ItemPos // Optionally overrides the default position used for logging.
	sev ErrorSeverity
}

type ErrorList []Error

func (e *ErrorList) createIfNecessary() *ErrorList {
	if e == nil {
		e = &ErrorList{}
	}
	return e
}

// AddL appends an existing error list to e, and returns e itself.
func (e *ErrorList) AddL(err *ErrorList) *ErrorList {
	if err != nil {
		e = e.createIfNecessary()
		*e = append(*e, (*err)...)
	}
	return e
}

// AddF appends a formatted error to e, and returns e itself.
func (e *ErrorList) AddF(sev ErrorSeverity, format string, a ...interface{}) *ErrorList {
	return e.AddFAt(nil, sev, format, a...)
}

// AddFAt appends a formatted error at the given code position to e, and
// returns e itself.
func (e *ErrorList) AddFAt(pos *ItemPos, sev ErrorSeverity, format string, a ...interface{}) *ErrorList {
	e = e.createIfNecessary()
	*e = append(*e, Error{s: fmt.Sprintf(format, a...), pos: pos, sev: sev})
	return e
}

// NewErrorList creates a new error list from the given existing error.
func NewErrorList(sev ErrorSeverity, err error) *ErrorList {
	return &ErrorList{Error{s: err.Error(), sev: sev}}
}

// ErrorListF creates a new error list from the given format string.
func ErrorListF(sev ErrorSeverity, format string, a ...interface{}) *ErrorList {
	return ErrorListFAt(nil, sev, format, a...)
}

// ErrorListFAt creates a new error list with an error from the given format
// string at the given code position.
func ErrorListFAt(pos *ItemPos, sev ErrorSeverity, format string, a ...interface{}) *ErrorList {
	return &ErrorList{Error{s: fmt.Sprintf(format, a...), pos: pos, sev: sev}}
}
