package usage

import "testing"

func TestNormalize(t *testing.T) {
	u := Normalize(Usage{Input: 3, Output: 4})
	if u.Total != 7 {
		t.Fatalf("expected total to be 7, got %d", u.Total)
	}

	u = Normalize(Usage{Input: 1, Output: 2, Total: 10})
	if u.Total != 10 {
		t.Fatalf("expected total to remain 10, got %d", u.Total)
	}
}

func TestAdd(t *testing.T) {
	a := Usage{Input: 10, Output: 20, CacheReadInput: 100, CacheCreationInput: 50}

	// Add nil returns a unchanged.
	got := Add(a, nil)
	if got != a {
		t.Fatalf("Add(a, nil) = %+v, want %+v", got, a)
	}

	// Add two usages sums all fields and normalizes Total.
	b := &Usage{Input: 5, Output: 10, CacheReadInput: 200, CacheCreationInput: 30}
	got = Add(a, b)
	want := Usage{
		Input:              15,
		Output:             30,
		Total:              45,
		CacheReadInput:     300,
		CacheCreationInput: 80,
	}
	if got != want {
		t.Fatalf("Add(a, b) = %+v, want %+v", got, want)
	}
}

func TestAdd_Accumulate(t *testing.T) {
	// Simulate accumulating usage across multiple loop steps.
	var total Usage
	steps := []*Usage{
		{Input: 50, Output: 10, CacheReadInput: 0, CacheCreationInput: 500},
		{Input: 5, Output: 15, CacheReadInput: 500, CacheCreationInput: 10},
		{Input: 3, Output: 20, CacheReadInput: 510, CacheCreationInput: 5},
	}
	for _, s := range steps {
		total = Add(total, s)
	}
	if total.Input != 58 {
		t.Fatalf("expected Input=58, got %d", total.Input)
	}
	if total.CacheReadInput != 1010 {
		t.Fatalf("expected CacheReadInput=1010, got %d", total.CacheReadInput)
	}
	if total.CacheCreationInput != 515 {
		t.Fatalf("expected CacheCreationInput=515, got %d", total.CacheCreationInput)
	}
	if total.Total != 103 {
		t.Fatalf("expected Total=103 (58+45), got %d", total.Total)
	}
}
