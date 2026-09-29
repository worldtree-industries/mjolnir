package set

var _ Set[int] = (*setImpl[int])(nil)

type Set[T comparable] interface {
	Add(item T)
	Remove(item T)
	Contains(item T) bool
	Len() int
	Items() []T

	Union(other Set[T]) Set[T]
	Intersection(other Set[T]) Set[T]
	Difference(other Set[T]) Set[T]
	SymmetricDifference(other Set[T]) Set[T]
}

// Set is a generic set implementation.
type setImpl[T comparable] struct {
	values map[T]struct{}
}

// New creates a new set.
func New[T comparable](items ...T) Set[T] {
	s := &setImpl[T]{
		values: make(map[T]struct{}),
	}
	for _, item := range items {
		s.Add(item)
	}
	return s
}

// Add adds an item to the set.
func (s *setImpl[T]) Add(item T) {
	s.values[item] = struct{}{}
}

// Remove removes an item from the set.
func (s *setImpl[T]) Remove(item T) {
	delete(s.values, item)
}

// Contains checks if an item is in the set.
func (s *setImpl[T]) Contains(item T) bool {
	_, ok := s.values[item]
	return ok
}

// Len returns the number of items in the set.
func (s *setImpl[T]) Len() int {
	return len(s.values)
}

// Items returns a slice of all items in the set.
func (s *setImpl[T]) Items() []T {
	items := make([]T, 0, len(s.values))
	for item := range s.values {
		items = append(items, item)
	}
	return items
}

// Union returns a new set containing all items from both sets.
func (s *setImpl[T]) Union(other Set[T]) Set[T] {
	union := New(s.Items()...)
	for _, item := range other.Items() {
		union.Add(item)
	}
	return union
}

// Intersection returns a new set containing all items common to both sets.
func (s *setImpl[T]) Intersection(other Set[T]) Set[T] {
	intersection := New[T]()
	for _, item := range s.Items() {
		if other.Contains(item) {
			intersection.Add(item)
		}
	}
	return intersection
}

// Difference returns a new set containing all items in the first set that are not in the second set.
func (s *setImpl[T]) Difference(other Set[T]) Set[T] {
	difference := New[T]()
	for item := range s.values {
		if !other.Contains(item) {
			difference.Add(item)
		}
	}
	return difference
}

// SymmetricDifference returns a new set containing all items in either set, but not both.
func (s *setImpl[T]) SymmetricDifference(other Set[T]) Set[T] {
	return s.Difference(other).Union(other.Difference(s))
}
