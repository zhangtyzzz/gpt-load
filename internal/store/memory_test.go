package store

import "testing"

func TestMemoryStoreReplaceList(t *testing.T) {
	t.Parallel()

	s := NewMemoryStore()
	if err := s.LPush("keys", 1, 2); err != nil {
		t.Fatalf("LPush() error = %v", err)
	}
	if err := s.ReplaceList("keys", 3, 4); err != nil {
		t.Fatalf("ReplaceList() error = %v", err)
	}
	if length, err := s.LLen("keys"); err != nil || length != 2 {
		t.Fatalf("LLen() = %d, %v; want 2, nil", length, err)
	}
	if got, err := s.Rotate("keys"); err != nil || got != "4" {
		t.Fatalf("Rotate() = %q, %v; want 4, nil", got, err)
	}
	for _, value := range []int{3, 4} {
		if contains, err := s.LContains("keys", value); err != nil || !contains {
			t.Errorf("LContains(%d) = %v, %v; want true, nil", value, contains, err)
		}
	}
	if contains, err := s.LContains("keys", 1); err != nil || contains {
		t.Errorf("LContains(1) = %v, %v; want false, nil", contains, err)
	}

	if err := s.ReplaceList("keys"); err != nil {
		t.Fatalf("ReplaceList(empty) error = %v", err)
	}
	if length, err := s.LLen("keys"); err != nil || length != 0 {
		t.Fatalf("LLen() after empty replace = %d, %v; want 0, nil", length, err)
	}
}
