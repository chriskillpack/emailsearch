package emailsearch

import "iter"

// A unordered Set.
type Set[E comparable] struct {
	elems map[E]struct{}
}

func NewSet[E comparable]() *Set[E] {
	return &Set[E]{
		elems: make(map[E]struct{}),
	}
}

func (s *Set[E]) Insert(item E) {
	s.elems[item] = struct{}{}
}

func (s *Set[E]) Remove(item E) {
	delete(s.elems, item)
}

func (s *Set[E]) Has(item E) bool {
	_, has := s.elems[item]
	return has
}

func (s *Set[E]) Elems() iter.Seq[E] {
	return func(yield func(E) bool) {
		for k := range s.elems {
			if !yield(k) {
				return
			}
		}
	}
}

// Union returns a new set which is the union of s and a
func (s *Set[E]) Union(a *Set[E]) *Set[E] {
	r := NewSet[E]()
	for k := range s.elems {
		r.Insert(k)
	}
	for k := range a.elems {
		r.Insert(k)
	}

	return r
}

// Intersect returns a new set which is the intersection of s and a
func (s *Set[E]) Intersect(a *Set[E]) *Set[E] {
	r := NewSet[E]()
	for k := range a.elems {
		if s.Has(k) {
			r.Insert(k)
		}
	}

	return r
}

// Difference returns a new set which the difference of s - a
func (s *Set[E]) Difference(a *Set[E]) *Set[E] {
	r := NewSet[E]()
	for k := range s.elems {
		if !a.Has(k) {
			r.Insert(k)
		}
	}

	return r
}
