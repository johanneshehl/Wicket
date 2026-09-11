package main

import (
	"strings"
	"testing"
)

const testImport = "import /etc/caddy/wicket/*.caddy"

const sample = `app.example.com {
	basic_auth {
		admin $2a$14$abc
	}
	reverse_proxy 127.0.0.1:3000
}

other.example.com {
	reverse_proxy 127.0.0.1:4000
}
import /etc/caddy/wicket/*.caddy
`

func TestRewriteProtectsBlockAndMovesImport(t *testing.T) {
	out := rewriteCaddyfile(sample, map[string]bool{"app.example.com": true}, testImport)
	lines := strings.Split(out, "\n")
	if lines[1] != testImport {
		t.Fatalf("import not at the top:\n%s", out)
	}
	if strings.Count(out, testImport) != 1 {
		t.Fatalf("import duplicated:\n%s", out)
	}
	if !strings.Contains(out, "app.example.com {\n\t"+importMarker+"\n") {
		t.Fatalf("import wicket not added:\n%s", out)
	}
	if !strings.Contains(out, "\t"+disabledPrefix+"\tbasic_auth {") || !strings.Contains(out, "\t\t"+disabledPrefix+"\t\tadmin") {
		t.Fatalf("basic_auth not disabled:\n%s", out)
	}
	if strings.Contains(strings.Split(out, "other.example.com")[1], "import wicket") {
		t.Fatalf("unrelated block touched:\n%s", out)
	}
}

func TestRewriteIsIdempotentAndReversible(t *testing.T) {
	protect := map[string]bool{"app.example.com": true}
	once := rewriteCaddyfile(sample, protect, testImport)
	twice := rewriteCaddyfile(once, protect, testImport)
	if once != twice {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, twice)
	}
	back := rewriteCaddyfile(once, map[string]bool{}, testImport)
	if strings.Contains(back, "import wicket") || strings.Contains(back, disabledPrefix) {
		t.Fatalf("not reversed:\n%s", back)
	}
	if !strings.Contains(back, "\tbasic_auth {\n\t\tadmin $2a$14$abc\n\t}") {
		t.Fatalf("basic_auth not restored:\n%s", back)
	}
}

func TestRewriteKeepsManualImport(t *testing.T) {
	in := "a.example.com {\n\timport wicket\n\treverse_proxy 127.0.0.1:1\n}\n"
	out := rewriteCaddyfile(in, map[string]bool{"a.example.com": true}, testImport)
	if strings.Count(out, "import wicket") != 1 {
		t.Fatalf("manual import duplicated:\n%s", out)
	}
	// a manual import is left alone when the site is removed
	if !strings.Contains(rewriteCaddyfile(out, nil, testImport), "\timport wicket\n") {
		t.Fatal("manual import removed")
	}
}

func TestRewriteAfterGlobalOptions(t *testing.T) {
	in := "{\n\temail me@example.com\n}\n\na.example.com {\n\treverse_proxy 127.0.0.1:1\n}\n"
	out := rewriteCaddyfile(in, nil, testImport)
	if !strings.HasPrefix(out, "{\n\temail me@example.com\n}\n\n"+importComment+"\n"+testImport+"\n") {
		t.Fatalf("import not placed after global options:\n%s", out)
	}
}

func TestParseBlocksHostLists(t *testing.T) {
	blocks := parseBlocks(strings.Split("a.example.com, https://b.example.com:443 {\n\trespond ok\n}\n(snip) {\n}\n", "\n"))
	if blockFor(blocks, "b.example.com") != 0 || blockFor(blocks, "snip") != -1 {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}
