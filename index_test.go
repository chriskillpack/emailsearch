package emailsearch

import (
	"reflect"
	"testing"
)

func TestIntersectWordResults(t *testing.T) {
	cases := []struct {
		Name     string
		Results  []map[int][]QueryWordMatch
		Expected map[int][]QueryWordMatch
	}{
		{
			Name:     "Empty set",
			Results:  []map[int][]QueryWordMatch{},
			Expected: nil,
		},
		{
			Name: "Single map",
			Results: []map[int][]QueryWordMatch{
				{
					1: {{Word: "test", Offset: 1}},
				},
			},
			Expected: map[int][]QueryWordMatch{
				1: {{Word: "test", Offset: 1}},
			},
		},
		{
			Name: "Multiple maps with intersection",
			Results: []map[int][]QueryWordMatch{
				{
					1: {{Word: "test1", Offset: 1}},
					2: {{Word: "test2", Offset: 2}},
				},
				{
					1: {{Word: "test3", Offset: 3}},
					3: {{Word: "test4", Offset: 4}},
				},
			},
			Expected: map[int][]QueryWordMatch{
				1: {{Word: "test1", Offset: 1}, {Word: "test3", Offset: 3}},
			},
		},
		{
			Name: "Multiple maps without intersection",
			Results: []map[int][]QueryWordMatch{
				{
					1: {{Word: "test1", Offset: 1}},
				},
				{
					2: {{Word: "test2", Offset: 2}},
				},
			},
			Expected: map[int][]QueryWordMatch{},
		},
		{
			Name: "Multiple maps intersecting",
			Results: []map[int][]QueryWordMatch{
				{
					1: {{Word: "test1", Offset: 10}},
				},
				{
					1: {{Word: "test3", Offset: 15}},
				},
				{
					1: {{Word: "test2", Offset: 7}},
				},
			},
			Expected: map[int][]QueryWordMatch{
				1: {
					{Word: "test1", Offset: 10},
					{Word: "test3", Offset: 15},
					{Word: "test2", Offset: 7},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			actual := intersectWordResults(tc.Results)
			if got, want := len(actual), len(tc.Expected); got != want {
				t.Errorf("expected %d results, got %d", want, got)
			}

			if !reflect.DeepEqual(actual, tc.Expected) {
				t.Errorf("expected %v, got %v", tc.Expected, actual)
			}
		})
	}
}
