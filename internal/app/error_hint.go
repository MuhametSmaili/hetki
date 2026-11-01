package app

import "errors"

type hintedError struct {
	cause error
	hint  string
}

func (e *hintedError) Error() string { return e.cause.Error() }
func (e *hintedError) Unwrap() error { return e.cause }

// WithHint keeps a suggestion separate from the error text and preserves its cause.
// The renderer must still sanitize both fields. A nil error remains nil.
func WithHint(err error, hint string) error {
	if err == nil {
		return nil
	}
	return &hintedError{cause: err, hint: hint}
}

// ErrorHint returns the first structured hint in the error chain, if present.
func ErrorHint(err error) string {
	if hinted, ok := errors.AsType[*hintedError](err); ok {
		return hinted.hint
	}
	return ""
}
