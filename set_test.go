package column

import (
	"sort"
	"testing"
)

func TestSetBasicOperations(t *testing.T) {
	s := NewSet[int]()

	// Test empty set
	if s.Has(1) {
		t.Error("Empty set should not contain any elements")
	}

	// Test Insert and Has
	s.Insert(1)
	if !s.Has(1) {
		t.Error("Set should contain inserted element")
	}
	if s.Has(2) {
		t.Error("Set should not contain elements that weren't inserted")
	}

	// Test duplicate insertion
	s.Insert(1)
	s.Insert(1)
	count := 0
	for range s.Elems() {
		count++
	}
	if count != 1 {
		t.Errorf("Set should contain only one instance of element after duplicate insertions, got %d", count)
	}

	// Test Remove
	s.Remove(1)
	if s.Has(1) {
		t.Error("Set should not contain removed element")
	}

	// Test removing non-existent element
	s.Remove(2) // Should not panic
}

func TestSetElems(t *testing.T) {
	s := NewSet[int]()
	expected := []int{1, 2, 3, 4, 5}

	for _, v := range expected {
		s.Insert(v)
	}

	// Collect elements into a slice for comparison
	var result []int
	for v := range s.Elems() {
		result = append(result, v)
	}

	// Sort both slices since set doesn't guarantee order
	sort.Ints(result)
	sort.Ints(expected)

	if len(result) != len(expected) {
		t.Errorf("Expected %d elements, got %d", len(expected), len(result))
	}

	for i := range expected {
		if result[i] != expected[i] {
			t.Errorf("Element mismatch at position %d: expected %d, got %d", i, expected[i], result[i])
		}
	}
}

func TestSetOperations(t *testing.T) {
	tests := []struct {
		name      string
		set1      []int
		set2      []int
		union     []int
		intersect []int
		diff      []int
	}{
		{
			name:      "Basic sets",
			set1:      []int{1, 2, 3},
			set2:      []int{3, 4, 5},
			union:     []int{1, 2, 3, 4, 5},
			intersect: []int{3},
			diff:      []int{1, 2},
		},
		{
			name:      "Disjoint sets",
			set1:      []int{1, 2},
			set2:      []int{3, 4},
			union:     []int{1, 2, 3, 4},
			intersect: []int{},
			diff:      []int{1, 2},
		},
		{
			name:      "Identical sets",
			set1:      []int{1, 2, 3},
			set2:      []int{1, 2, 3},
			union:     []int{1, 2, 3},
			intersect: []int{1, 2, 3},
			diff:      []int{},
		},
		{
			name:      "Empty sets",
			set1:      []int{},
			set2:      []int{},
			union:     []int{},
			intersect: []int{},
			diff:      []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := NewSet[int]()
			s2 := NewSet[int]()

			// Populate sets
			for _, v := range tt.set1 {
				s1.Insert(v)
			}
			for _, v := range tt.set2 {
				s2.Insert(v)
			}

			// Test Union
			union := s1.Union(s2)
			if !compareSliceToSet(t, tt.union, union) {
				t.Errorf("Union failed for %s", tt.name)
			}

			// Test Intersection
			intersect := s1.Intersect(s2)
			if !compareSliceToSet(t, tt.intersect, intersect) {
				t.Errorf("Intersection failed for %s", tt.name)
			}

			// Test Difference
			diff := s1.Difference(s2)
			if !compareSliceToSet(t, tt.diff, diff) {
				t.Errorf("Difference failed for %s", tt.name)
			}
		})
	}
}

// Helper function to compare a slice with a set
func compareSliceToSet[E comparable](t *testing.T, expected []E, set *Set[E]) bool {
	t.Helper()

	// Convert expected slice to map for easier comparison
	expectedMap := make(map[E]struct{})
	for _, v := range expected {
		expectedMap[v] = struct{}{}
	}

	// Check if every element in the set is in expected
	for v := range set.Elems() {
		if _, ok := expectedMap[v]; !ok {
			t.Errorf("Unexpected element in set: %v", v)
			return false
		}
		delete(expectedMap, v)
	}

	// Check if there are any remaining expected elements
	if len(expectedMap) > 0 {
		t.Errorf("Missing expected elements: %v", expectedMap)
		return false
	}

	return true
}
