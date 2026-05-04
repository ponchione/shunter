package schema

import (
	"strings"
	"testing"
)

// --- Story 2.1: Tag parsing ---

func TestParseTagEmpty(t *testing.T) {
	td, err := ParseTag("")
	if err != nil {
		t.Fatalf("empty tag: %v", err)
	}
	if td.PrimaryKey || td.AutoIncrement || td.Unique || td.Index || td.Exclude {
		t.Fatal("empty tag should have all flags false")
	}
	if td.IndexName != "" || td.NameOverride != "" {
		t.Fatal("empty tag should have empty string fields")
	}
}

func TestParseTagPrimaryKey(t *testing.T) {
	td, err := ParseTag("primarykey")
	if err != nil {
		t.Fatalf("primarykey: %v", err)
	}
	if !td.PrimaryKey {
		t.Fatal("PrimaryKey should be true")
	}
}

func TestParseTagPrimaryKeyAutoIncrement(t *testing.T) {
	td, err := ParseTag("primarykey,autoincrement")
	if err != nil {
		t.Fatalf("primarykey,autoincrement: %v", err)
	}
	if !td.PrimaryKey || !td.AutoIncrement {
		t.Fatal("both PrimaryKey and AutoIncrement should be true")
	}
}

func TestParseTagParametricIndex(t *testing.T) {
	td, err := ParseTag("index:guild_score")
	if err != nil {
		t.Fatalf("index:guild_score: %v", err)
	}
	if td.IndexName != "guild_score" {
		t.Fatalf("IndexName = %q, want 'guild_score'", td.IndexName)
	}
}

func TestParseTagParametricName(t *testing.T) {
	td, err := ParseTag("name:player_id")
	if err != nil {
		t.Fatalf("name:player_id: %v", err)
	}
	if td.NameOverride != "player_id" {
		t.Fatalf("NameOverride = %q, want 'player_id'", td.NameOverride)
	}
}

func TestParseTagMixed(t *testing.T) {
	td, err := ParseTag("name:player_id,primarykey")
	if err != nil {
		t.Fatalf("mixed: %v", err)
	}
	if !td.PrimaryKey || td.NameOverride != "player_id" {
		t.Fatalf("unexpected: PK=%v Name=%q", td.PrimaryKey, td.NameOverride)
	}
}

func TestParseTagUniqueNamedIndex(t *testing.T) {
	td, err := ParseTag("unique,index:guild_score")
	if err != nil {
		t.Fatalf("unique,index: %v", err)
	}
	if !td.Unique || td.IndexName != "guild_score" {
		t.Fatalf("unexpected: Unique=%v IndexName=%q", td.Unique, td.IndexName)
	}
}

func TestParseTagExclude(t *testing.T) {
	td, err := ParseTag("-")
	if err != nil {
		t.Fatalf("exclude: %v", err)
	}
	if !td.Exclude {
		t.Fatal("Exclude should be true")
	}
}

func TestParseTagUnknownDirective(t *testing.T) {
	_, err := ParseTag("foo")
	if err == nil {
		t.Fatal("unknown directive should error")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Fatalf("error should mention 'foo': %v", err)
	}
}

func TestParseTagEmptyParametricIndex(t *testing.T) {
	_, err := ParseTag("index:")
	if err == nil {
		t.Fatal("empty index: should error")
	}
}

func TestParseTagEmptyParametricName(t *testing.T) {
	_, err := ParseTag("name:")
	if err == nil {
		t.Fatal("empty name: should error")
	}
}

// --- Story 2.2: Tag validation ---

func TestParseTagExcludeNotAlone(t *testing.T) {
	for _, tag := range []string{"-,index", "-,primarykey"} {
		_, err := ParseTag(tag)
		if err == nil {
			t.Fatalf("%q should fail: exclude must appear alone", tag)
		}
	}
}

func TestParseTagPrimaryKeyWithIndex(t *testing.T) {
	for _, tag := range []string{"primarykey,index", "primarykey,index:foo"} {
		_, err := ParseTag(tag)
		if err == nil {
			t.Fatalf("%q should fail: PK cannot combine with index", tag)
		}
	}
}

func TestParseTagDuplicateDirectives(t *testing.T) {
	for _, tag := range []string{"index,index", "unique,unique"} {
		_, err := ParseTag(tag)
		if err == nil {
			t.Fatalf("%q should fail: duplicate directive", tag)
		}
	}
}

func TestParseTagPlainAndNamedIndex(t *testing.T) {
	_, err := ParseTag("index,index:foo")
	if err == nil {
		t.Fatal("plain + named index should fail")
	}
}

func TestParseTagUniqueWithPlainIndex(t *testing.T) {
	_, err := ParseTag("unique,index")
	if err == nil {
		t.Fatal("unique + plain index should fail")
	}
}

func TestParseTagPrimaryKeyAutoIncrementValid(t *testing.T) {
	_, err := ParseTag("primarykey,autoincrement")
	if err != nil {
		t.Fatalf("primarykey,autoincrement should be valid: %v", err)
	}
}

func TestParseTagUniqueNamedIndexValid(t *testing.T) {
	_, err := ParseTag("unique,index:guild_score")
	if err != nil {
		t.Fatalf("unique,index:guild_score should be valid: %v", err)
	}
}

// --- DefaultIndexName ---

func TestDefaultIndexNamePK(t *testing.T) {
	if got := DefaultIndexName("id", true, true); got != "pk" {
		t.Fatalf("PK index name = %q, want 'pk'", got)
	}
}

func TestDefaultIndexNameUnique(t *testing.T) {
	if got := DefaultIndexName("email", false, true); got != "email_uniq" {
		t.Fatalf("unique index name = %q, want 'email_uniq'", got)
	}
}

func TestDefaultIndexNamePlain(t *testing.T) {
	if got := DefaultIndexName("name", false, false); got != "name_idx" {
		t.Fatalf("plain index name = %q, want 'name_idx'", got)
	}
}
