package subscription

import "testing"

func TestTableIndexAddLookup(t *testing.T) {
	ti := NewTableIndex()
	ti.Add(1, hashN(1))
	got := ti.Lookup(1)
	if len(got) != 1 || got[0] != hashN(1) {
		t.Fatalf("Lookup = %v", got)
	}
}

func TestTableIndexMultipleHashes(t *testing.T) {
	ti := NewTableIndex()
	ti.Add(1, hashN(1))
	ti.Add(1, hashN(2))
	if got := ti.Lookup(1); len(got) != 2 {
		t.Fatalf("Lookup = %v, want 2", got)
	}
}

func TestTableIndexLookupEmpty(t *testing.T) {
	ti := NewTableIndex()
	if got := ti.Lookup(1); len(got) != 0 {
		t.Fatalf("Lookup empty = %v, want empty", got)
	}
}

func TestTableIndexRemove(t *testing.T) {
	ti := NewTableIndex()
	ti.Add(1, hashN(1))
	ti.Remove(1, hashN(1))
	if got := ti.Lookup(1); len(got) != 0 {
		t.Fatalf("after remove Lookup = %v", got)
	}
}

func TestTableIndexRemoveCleansUp(t *testing.T) {
	ti := NewTableIndex()
	ti.Add(1, hashN(1))
	ti.Remove(1, hashN(1))
	if _, ok := ti.tables[1]; ok {
		t.Fatal("empty table entry not cleaned up")
	}
}
