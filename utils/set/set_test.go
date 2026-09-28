package set

import (
	"sort"
	"testing"
)

func TestSet_New(t *testing.T) {
	tests := []struct {
		name     string
		items    []int
		expected []int
	}{
		{"empty set", []int{}, []int{}},
		{"single item", []int{1}, []int{1}},
		{"multiple items", []int{1, 2, 3}, []int{1, 2, 3}},
		{"duplicate items", []int{1, 2, 2, 3, 1}, []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.items...)

			if s.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), s.Len())
			}

			for _, item := range tt.expected {
				if !s.Contains(item) {
					t.Errorf("expected set to contain %v", item)
				}
			}
		})
	}
}

func TestSet_Add(t *testing.T) {
	tests := []struct {
		name          string
		initial       []int
		add           []int
		expectedLen   int
		expectedItems []int
	}{
		{"add to empty set", []int{}, []int{1}, 1, []int{1}},
		{"add duplicate", []int{1}, []int{1}, 1, []int{1}},
		{"add multiple", []int{1}, []int{2, 3}, 3, []int{1, 2, 3}},
		{"add with duplicates", []int{1, 2}, []int{2, 3, 3}, 3, []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.initial...)
			for _, item := range tt.add {
				s.Add(item)
			}

			if s.Len() != tt.expectedLen {
				t.Errorf("expected length %d, got %d", tt.expectedLen, s.Len())
			}

			for _, item := range tt.expectedItems {
				if !s.Contains(item) {
					t.Errorf("expected set to contain %v", item)
				}
			}
		})
	}
}

func TestSet_Remove(t *testing.T) {
	tests := []struct {
		name             string
		initial          []int
		remove           []int
		expectedLen      int
		shouldContain    []int
		shouldNotContain []int
	}{
		{"remove existing item", []int{1, 2, 3}, []int{2}, 2, []int{1, 3}, []int{2}},
		{"remove non-existing item", []int{1, 2, 3}, []int{4}, 3, []int{1, 2, 3}, []int{4}},
		{"remove all items", []int{1, 2, 3}, []int{1, 2, 3}, 0, []int{}, []int{1, 2, 3}},
		{"remove from empty set", []int{}, []int{1}, 0, []int{}, []int{1}},
		{"remove multiple", []int{1, 2, 3, 4, 5}, []int{2, 4}, 3, []int{1, 3, 5}, []int{2, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.initial...)
			for _, item := range tt.remove {
				s.Remove(item)
			}

			if s.Len() != tt.expectedLen {
				t.Errorf("expected length %d, got %d", tt.expectedLen, s.Len())
			}

			for _, item := range tt.shouldContain {
				if !s.Contains(item) {
					t.Errorf("expected set to contain %v", item)
				}
			}

			for _, item := range tt.shouldNotContain {
				if s.Contains(item) {
					t.Errorf("expected set to not contain %v", item)
				}
			}
		})
	}
}

func TestSet_Contains(t *testing.T) {
	tests := []struct {
		name     string
		items    []int
		check    int
		expected bool
	}{
		{"contains existing item", []int{1, 2, 3}, 2, true},
		{"does not contain missing item", []int{1, 2, 3}, 4, false},
		{"empty set contains nothing", []int{}, 1, false},
		{"contains first item", []int{1, 2, 3}, 1, true},
		{"contains last item", []int{1, 2, 3}, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.items...)
			result := s.Contains(tt.check)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSet_Len(t *testing.T) {
	tests := []struct {
		name     string
		items    []int
		expected int
	}{
		{"empty set", []int{}, 0},
		{"single item", []int{1}, 1},
		{"multiple items", []int{1, 2, 3}, 3},
		{"duplicates", []int{1, 1, 2, 2, 3}, 3},
		{"many items", []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.items...)
			if s.Len() != tt.expected {
				t.Errorf("expected length %d, got %d", tt.expected, s.Len())
			}
		})
	}
}

func TestSet_Items(t *testing.T) {
	tests := []struct {
		name     string
		items    []int
		expected []int
	}{
		{"empty set", []int{}, []int{}},
		{"single item", []int{1}, []int{1}},
		{"multiple items", []int{1, 2, 3}, []int{1, 2, 3}},
		{"with duplicates", []int{3, 1, 2, 1, 3}, []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.items...)
			result := s.Items()

			sort.Ints(result)
			sort.Ints(tt.expected)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
				return
			}

			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("expected %v, got %v", tt.expected, result)
					return
				}
			}
		})
	}
}

func TestSet_Union(t *testing.T) {
	tests := []struct {
		name     string
		set1     []int
		set2     []int
		expected []int
	}{
		{"empty sets", []int{}, []int{}, []int{}},
		{"first empty", []int{}, []int{1, 2}, []int{1, 2}},
		{"second empty", []int{1, 2}, []int{}, []int{1, 2}},
		{"no overlap", []int{1, 2}, []int{3, 4}, []int{1, 2, 3, 4}},
		{"partial overlap", []int{1, 2, 3}, []int{3, 4, 5}, []int{1, 2, 3, 4, 5}},
		{"complete overlap", []int{1, 2, 3}, []int{1, 2, 3}, []int{1, 2, 3}},
		{"subset", []int{1, 2, 3, 4, 5}, []int{2, 3}, []int{1, 2, 3, 4, 5}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)
			result := s1.Union(s2)

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected union to contain %v", item)
				}
			}

			// Verify original sets unchanged
			if s1.Len() != len(tt.set1) {
				t.Error("original set s1 was modified")
			}
			if s2.Len() != len(tt.set2) {
				t.Error("original set s2 was modified")
			}
		})
	}
}

func TestSet_Intersection(t *testing.T) {
	tests := []struct {
		name     string
		set1     []int
		set2     []int
		expected []int
	}{
		{"empty sets", []int{}, []int{}, []int{}},
		{"first empty", []int{}, []int{1, 2}, []int{}},
		{"second empty", []int{1, 2}, []int{}, []int{}},
		{"no overlap", []int{1, 2}, []int{3, 4}, []int{}},
		{"partial overlap", []int{1, 2, 3}, []int{3, 4, 5}, []int{3}},
		{"complete overlap", []int{1, 2, 3}, []int{1, 2, 3}, []int{1, 2, 3}},
		{"subset", []int{1, 2, 3, 4, 5}, []int{2, 3}, []int{2, 3}},
		{"multiple overlap", []int{1, 2, 3, 4, 5}, []int{2, 3, 4, 6, 7}, []int{2, 3, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)
			result := s1.Intersection(s2)

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected intersection to contain %v", item)
				}
			}

			// Verify original sets unchanged
			if s1.Len() != len(tt.set1) {
				t.Error("original set s1 was modified")
			}
			if s2.Len() != len(tt.set2) {
				t.Error("original set s2 was modified")
			}
		})
	}
}

func TestSet_Difference(t *testing.T) {
	tests := []struct {
		name     string
		set1     []int
		set2     []int
		expected []int
	}{
		{"empty sets", []int{}, []int{}, []int{}},
		{"first empty", []int{}, []int{1, 2}, []int{}},
		{"second empty", []int{1, 2}, []int{}, []int{1, 2}},
		{"no overlap", []int{1, 2}, []int{3, 4}, []int{1, 2}},
		{"partial overlap", []int{1, 2, 3}, []int{3, 4, 5}, []int{1, 2}},
		{"complete overlap", []int{1, 2, 3}, []int{1, 2, 3}, []int{}},
		{"subset removed", []int{1, 2, 3, 4, 5}, []int{2, 3}, []int{1, 4, 5}},
		{"superset", []int{2, 3}, []int{1, 2, 3, 4, 5}, []int{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)
			result := s1.Difference(s2)

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected difference to contain %v", item)
				}
			}

			// Verify original sets unchanged
			if s1.Len() != len(tt.set1) {
				t.Error("original set s1 was modified")
			}
			if s2.Len() != len(tt.set2) {
				t.Error("original set s2 was modified")
			}
		})
	}
}

func TestSet_SymmetricDifference(t *testing.T) {
	tests := []struct {
		name     string
		set1     []int
		set2     []int
		expected []int
	}{
		{"empty sets", []int{}, []int{}, []int{}},
		{"first empty", []int{}, []int{1, 2}, []int{1, 2}},
		{"second empty", []int{1, 2}, []int{}, []int{1, 2}},
		{"no overlap", []int{1, 2}, []int{3, 4}, []int{1, 2, 3, 4}},
		{"partial overlap", []int{1, 2, 3}, []int{3, 4, 5}, []int{1, 2, 4, 5}},
		{"complete overlap", []int{1, 2, 3}, []int{1, 2, 3}, []int{}},
		{"single common", []int{1, 2, 3}, []int{3, 4, 5, 6}, []int{1, 2, 4, 5, 6}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)
			result := s1.SymmetricDifference(s2)

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected symmetric difference to contain %v", item)
				}
			}

			// Verify original sets unchanged
			if s1.Len() != len(tt.set1) {
				t.Error("original set s1 was modified")
			}
			if s2.Len() != len(tt.set2) {
				t.Error("original set s2 was modified")
			}
		})
	}
}

func TestSet_GenericString(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		set1      []string
		set2      []string
		expected  []string
	}{
		{"union no overlap", "union", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b", "c", "d"}},
		{"union with overlap", "union", []string{"a", "b", "c"}, []string{"c", "d", "e"}, []string{"a", "b", "c", "d", "e"}},
		{"intersection with overlap", "intersection", []string{"a", "b", "c"}, []string{"c", "d", "e"}, []string{"c"}},
		{"intersection no overlap", "intersection", []string{"a", "b"}, []string{"c", "d"}, []string{}},
		{"difference with overlap", "difference", []string{"a", "b", "c"}, []string{"c", "d", "e"}, []string{"a", "b"}},
		{"difference no overlap", "difference", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b"}},
		{"symmetric difference with overlap", "symmetric_difference", []string{"a", "b", "c"}, []string{"c", "d", "e"}, []string{"a", "b", "d", "e"}},
		{"symmetric difference no overlap", "symmetric_difference", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)

			var result Set[string]
			switch tt.operation {
			case "union":
				result = s1.Union(s2)
			case "intersection":
				result = s1.Intersection(s2)
			case "difference":
				result = s1.Difference(s2)
			case "symmetric_difference":
				result = s1.SymmetricDifference(s2)
			}

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected result to contain %s", item)
				}
			}
		})
	}
}

func TestSet_GenericStruct(t *testing.T) {
	type Point struct {
		X, Y int
	}

	tests := []struct {
		name      string
		operation string
		items     []Point
		expected  int
	}{
		{"add unique structs", "add", []Point{{1, 2}, {3, 4}, {5, 6}}, 3},
		{"add duplicate structs", "add", []Point{{1, 2}, {3, 4}, {1, 2}}, 2},
		{"contains struct", "contains", []Point{{1, 2}, {3, 4}}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.items...)

			if s.Len() != tt.expected {
				t.Errorf("expected length %d, got %d", tt.expected, s.Len())
			}

			for _, item := range tt.items[:tt.expected] {
				if !s.Contains(item) {
					t.Errorf("expected set to contain %v", item)
				}
			}
		})
	}
}

func TestSet_OperationChaining(t *testing.T) {
	tests := []struct {
		name     string
		set1     []int
		set2     []int
		set3     []int
		expected []int
	}{
		{
			"union then intersection",
			[]int{1, 2, 3},
			[]int{3, 4, 5},
			[]int{3, 5, 6, 7},
			[]int{3, 5},
		},
		{
			"difference then union",
			[]int{1, 2, 3, 4},
			[]int{3, 4},
			[]int{5, 6},
			[]int{1, 2, 5, 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s1 := New(tt.set1...)
			s2 := New(tt.set2...)
			s3 := New(tt.set3...)

			var result Set[int]
			if tt.name == "union then intersection" {
				result = s1.Union(s2).Intersection(s3)
			} else {
				result = s1.Difference(s2).Union(s3)
			}

			if result.Len() != len(tt.expected) {
				t.Errorf("expected length %d, got %d", len(tt.expected), result.Len())
			}

			for _, item := range tt.expected {
				if !result.Contains(item) {
					t.Errorf("expected result to contain %v", item)
				}
			}
		})
	}
}

func TestSet_InterfaceCompliance(t *testing.T) {
	var _ Set[int] = New[int]()
	var _ Set[string] = New[string]()
	var _ Set[bool] = New[bool]()
}

// Benchmark tests
func BenchmarkSetAdd(b *testing.B) {
	s := New[int]()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Add(i)
	}
}

func BenchmarkSetContains(b *testing.B) {
	s := New[int]()
	for i := range 1000 {
		s.Add(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Contains(i % 1000)
	}
}

func BenchmarkSetUnion(b *testing.B) {
	s1 := New[int]()
	s2 := New[int]()
	for i := range 500 {
		s1.Add(i)
		s2.Add(i + 250) // 50% overlap
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s1.Union(s2)
	}
}

func BenchmarkSetIntersection(b *testing.B) {
	s1 := New[int]()
	s2 := New[int]()
	for i := range 500 {
		s1.Add(i)
		s2.Add(i + 250)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s1.Intersection(s2)
	}
}

func BenchmarkSetDifference(b *testing.B) {
	s1 := New[int]()
	s2 := New[int]()
	for i := range 500 {
		s1.Add(i)
		s2.Add(i + 250)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s1.Difference(s2)
	}
}
