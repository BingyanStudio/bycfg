package set

import "maps"

type Set[T comparable] map[T]struct{}

func New[T comparable](items ...T) Set[T] {
	set := make(Set[T], len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return set
}

// return nil if p == nil
func FromPtr[T comparable](p *T) Set[T] {
	if p != nil {
		return Set[T]{*p: {}}
	}
	return nil
}

func (s Set[T]) Insert(t Set[T]) {
	maps.Copy(s, t)
}
