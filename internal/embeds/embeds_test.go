package embeds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const slidesURL = "https://docs.google.com/presentation/d/1jwmHV7I/edit?usp=sharing"

func widget(u string) string {
	return `<ac:structured-macro ac:name="widget" ac:schema-version="1"><ac:parameter ac:name="width">1400</ac:parameter>` +
		`<ac:parameter ac:name="url"><ri:url ri:value="` + u + `" /></ac:parameter></ac:structured-macro>`
}

func TestMatch(t *testing.T) {
	embeds := []Embed{{URL: "https://docs.google.com/presentation/d/1jwmHV7I/view", File: "/a.pdf"}, {URL: "OTHERID", File: "/b.pdf"}}
	for u, want := range map[string]string{
		slidesURL: "/a.pdf", // another link to the same document
		"https://docs.google.com/presentation/d/1jwmHV7I/edit#slide=id.p": "/a.pdf",
		"https://docs.google.com/presentation/d/OTHERID/pub?start=false":  "/b.pdf", // by ID alone
		"https://example.com/x": "",
	} {
		got := ""
		if e := Match(embeds, u); e != nil {
			got = e.File
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", u, got, want)
		}
	}
}

func TestApply(t *testing.T) {
	embeds := []Embed{{URL: "1jwmHV7I", File: "/docs/Intro deck.pdf"}, {URL: "img", File: "/docs/arch.png"}}
	panel := `<ac:structured-macro ac:name="panel"><ac:rich-text-body><p>In a panel:</p>` + widget(slidesURL) + `</ac:rich-text-body></ac:structured-macro>`
	body := `<p>Before</p>` + widget(strings.ReplaceAll(slidesURL, "&", "&amp;")) + `<ac:structured-macro ac:name="toc" />` +
		panel + widget("https://unmapped.example/x") +
		`<ac:structured-macro ac:name="iframe"><ac:parameter ac:name="url">https://host/img</ac:parameter></ac:structured-macro><p>After</p>`
	got, uses, unmapped := Apply(body, embeds)

	if len(uses) != 2 || uses[0].Title != "Intro deck.pdf" || uses[1].Title != "arch.png" {
		t.Errorf("uses = %+v", uses)
	}
	if len(unmapped) != 1 || unmapped[0] != "https://unmapped.example/x" {
		t.Errorf("unmapped = %v (only widgets are reported)", unmapped)
	}
	viewpdf := `<ac:structured-macro ac:name="viewpdf"><ac:parameter ac:name="name"><ri:attachment ri:filename="Intro deck.pdf" /></ac:parameter>` +
		`<ac:parameter ac:name="embedded-url">https://docs.google.com/presentation/d/1jwmHV7I/edit?usp=sharing</ac:parameter></ac:structured-macro>`
	if strings.Count(got, viewpdf) != 2 {
		t.Errorf("both widgets of the deck must become viewpdf:\n%s", got)
	}
	if !strings.Contains(got, `<ac:structured-macro ac:name="panel"><ac:rich-text-body><p>In a panel:</p>`+viewpdf+`</ac:rich-text-body></ac:structured-macro>`) {
		t.Errorf("the panel must stay around its widget:\n%s", got)
	}
	if !strings.Contains(got, `<ac:image><ri:attachment ri:filename="arch.png" /></ac:image><p>After</p>`) {
		t.Errorf("an image file must become an image:\n%s", got)
	}
	if !strings.HasPrefix(got, `<p>Before</p>`) || !strings.Contains(got, `<ac:structured-macro ac:name="toc" />`) || !strings.Contains(got, widget("https://unmapped.example/x")) {
		t.Errorf("the rest must stay as it was:\n%s", got)
	}

	if same, uses, _ := Apply(body, nil); same != body || uses != nil {
		t.Error("without embeds the body must not change")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	home, _ := os.UserHomeDir()
	path := filepath.Join(dir, "embeds.yaml")
	os.WriteFile(path, []byte("- url: ID1\n  file: ~/a.pdf\n- url: ID2\n  file: rel/b.pdf\n- url: ID3\n  file: /abs/c.pdf\n"), 0o644)
	embeds, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, "a.pdf"), filepath.Join(dir, "rel", "b.pdf"), "/abs/c.pdf"}
	for i, e := range embeds {
		if e.File != want[i] {
			t.Errorf("entry %d: %q, want %q", i, e.File, want[i])
		}
	}
	if e, err := Load(filepath.Join(dir, "none.yaml"), false); err != nil || e != nil {
		t.Errorf("a missing default file is no error: %v", err)
	}
	if _, err := Load(filepath.Join(dir, "none.yaml"), true); err == nil {
		t.Error("a missing named file is an error")
	}
	os.WriteFile(path, []byte("- url: ID1\n"), 0o644)
	if _, err := Load(path, true); err == nil {
		t.Error("an entry without a file is an error")
	}
}
