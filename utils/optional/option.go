package optional

// Option is a container for a value that may be absent. A present Option may
// legally hold a nil pointer; for value types, presence is decided by the
// constructor, never by the stored value.
//
// See docs/adr/0002-option-explicit-presence.md.
type Option[T any] struct {
	value T
	empty bool
}

// Of returns a present Option holding value. It is unconditional: a present
// Option may hold a nil pointer, and value types have no nil to reject.
func Of[T any](value T) Option[T] {
	return Option[T]{value: value}
}

// Empty returns an absent Option.
func Empty[T any]() Option[T] {
	return Option[T]{empty: true}
}

// IsPresent reports whether o holds a value.
func (o Option[T]) IsPresent() bool {
	return !o.empty
}

// Get returns the held value and reports whether o is present. For an empty
// Option it returns the zero value of T with false.
func (o Option[T]) Get() (T, bool) {
	return o.value, !o.empty
}

// Map returns an Option holding f(value) when o is present, and an empty
// Option when o is empty. f is not called on an empty Option.
func (o Option[T]) Map[U any](f func(T) U) Option[U] {
	if o.empty {
		return Empty[U]()
	}
	return Of(f(o.value))
}

// Filter returns o unchanged when it is present and f(value) is true, and an
// empty Option otherwise. f is not called on an empty Option.
func (o Option[T]) Filter(f func(T) bool) Option[T] {
	if o.empty || !f(o.value) {
		return Empty[T]()
	}
	return o
}

// OrElse returns the held value when o is present, and def when o is empty.
func (o Option[T]) OrElse(def T) T {
	if o.empty {
		return def
	}
	return o.value
}

// OrGet returns the held value when o is present, and fn() when o is empty.
// fn is not called on a present Option.
func (o Option[T]) OrGet(fn func() T) T {
	if o.empty {
		return fn()
	}
	return o.value
}
