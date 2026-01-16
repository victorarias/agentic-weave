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
