package webdavds

import "testing"

func TestNextToLast2(t *testing.T) {
	f, err := ParseShardFunc("/repo/flatfs/shard/v1/next-to-last/2")
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Func("ciqfexamplezab"); got != "za" {
		t.Fatalf("got %q want %q", got, "za")
	}
	if f.String() != "/repo/flatfs/shard/v1/next-to-last/2" {
		t.Fatalf("string mismatch: %q", f.String())
	}
}

func TestShardShortKeyPadding(t *testing.T) {
	f, _ := ParseShardFunc("/repo/flatfs/shard/v1/next-to-last/2")
	if got := f.Func("a"); len(got) != 2 {
		t.Fatalf("padding broken: %q", got)
	}
}

func TestShardPrefixSuffix(t *testing.T) {
	p, err := ParseShardFunc("/repo/flatfs/shard/v1/prefix/3")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Func("abcdef"); got != "abc" {
		t.Fatalf("prefix got %q", got)
	}
	s, err := ParseShardFunc("/repo/flatfs/shard/v1/suffix/2")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Func("abcdef"); got != "ef" {
		t.Fatalf("suffix got %q", got)
	}
}

func TestShardInvalid(t *testing.T) {
	for _, bad := range []string{"", "garbage", "/repo/flatfs/shard/v1/bogus/2", "/repo/flatfs/shard/v1/prefix/0"} {
		if _, err := ParseShardFunc(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
