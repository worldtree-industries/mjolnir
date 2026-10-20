package optional

import (
	"errors"
)

// ErrNilValue is returned by OfPtr when the pointer is nil. A present Option
// never holds a nil pointer.
var ErrNilValue = errors.New("optional: value is nil")

// Option is a container for a value that may be absent. It is intended for
// value types. A present Option never holds a nil pointer: pointer payloads
// enter through OfPtr, which rejects nil. The zero value of Option[T] is an
// empty Option.
//
// See docs/adr/0002-option-explicit-presence.md.
type Option[T any] struct {
	value   T
	present bool
}

// Of returns a present Option holding value. It is unconditional: value
// types — the intended use — have no nil to check. Pointer payloads enter
// through OfPtr, which rejects nil pointers.
func Of[T any](value T) Option[T] {
	return Option[T]{value: value, present: true}
}

// OfPtr returns a present Option holding *p. It returns ErrNilValue when p
// is nil: a present Option never holds a nil pointer. Go cannot check a
// generic T for nil, so the check lives here, where the pointer type is
// known at compile time and p == nil is a plain comparison.
func OfPtr[T any, P ~*T](p P) (Option[T], error) {
	if p == nil {
		return Empty[T](), ErrNilValue
	}
	return Option[T]{value: *p, present: true}, nil
}

// Empty returns an absent Option.
func Empty[T any]() Option[T] {
	return Option[T]{}
}

// IsPresent reports whether o holds a value.
func (o Option[T]) IsPresent() bool {
	return o.present
}

// Get returns the held value and reports whether o is present. For an empty
// Option it returns the zero value of T with false.
func (o Option[T]) Get() (T, bool) {
	return o.value, o.present
}

// Map returns an Option holding f(value) when o is present, and an empty
// Option when o is empty. f is not called on an empty Option. f must not
// return a nil pointer.
func (o Option[T]) Map[U any](f func(T) U) Option[U] {
	if !o.present {
		return Empty[U]()
	}
	return Of(f(o.value))
}

// Filter returns o unchanged when it is present and f(value) is true, and an
// empty Option otherwise. f is not called on an empty Option.
func (o Option[T]) Filter(f func(T) bool) Option[T] {
	if !o.present || !f(o.value) {
		return Empty[T]()
	}
	return o
}

// OrElse returns the held value when o is present, and def when o is empty.
func (o Option[T]) OrElse(def T) T {
	if !o.present {
		return def
	}
	return o.value
}

// OrGet returns the held value when o is present, and fn() when o is empty.
// fn is not called on a present Option.
func (o Option[T]) OrGet(fn func() T) T {
	if !o.present {
		return fn()
	}
	return o.value
}
