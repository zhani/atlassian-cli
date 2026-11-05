package mirrorsync

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/mirror"
)

const slidesURL = "https://docs.google.com/presentation/d/1jwmHV7I/edit?usp=sharing"

func widget(u string) string {
	return `<ac:structured-macro ac:name="widget" ac:schema-version="1"><ac:parameter ac:name="width">1400</ac:parameter>` +
		`<ac:parameter ac:name="url"><ri:url ri:value="` + u + `" /></ac:parameter></ac:structured-macro>`
}

func TestMatchEmbed(t *testing.T) {
	embeds := []Embed{{URL: "https://docs.google.com/presentation/d/1jwmHV7I/view", File: "/a.pdf"}, {URL: "OTHERID", File: "/b.pdf"}}
	for u, want := range map[string]string{
		slidesURL: "/a.pdf", // another link to the same document
		"https://docs.google.com/presentation/d/1jwmHV7I/edit#slide=id.p": "/a.pdf",
		"https://docs.google.com/presentation/d/OTHERID/pub?start=false":  "/b.pdf", // by ID alone
		"https://example.com/x": "",
	} {
		got := ""
		if e := matchEmbed(embeds, u); e != nil {
			got = e.File
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", u, got, want)
		}
	}
}

func TestApplyEmbeds(t *testing.T) {
	embeds := []Embed{{URL: "1jwmHV7I", File: "/docs/Intro deck.pdf"}, {URL: "img", File: "/docs/arch.png"}}
	panel := `<ac:structured-macro ac:name="panel"><ac:rich-text-body><p>In a panel:</p>` + widget(slidesURL) + `</ac:rich-text-body></ac:structured-macro>`
	body := `<p>Before</p>` + widget(strings.ReplaceAll(slidesURL, "&", "&amp;")) + `<ac:structured-macro ac:name="toc" />` +
		panel + widget("https://unmapped.example/x") +
		`<ac:structured-macro ac:name="iframe"><ac:parameter ac:name="url">https://host/img</ac:parameter></ac:structured-macro><p>After</p>`
	got, uses, unmapped := applyEmbeds(body, embeds)

	if len(uses) != 2 || uses[0].title != "Intro deck.pdf" || uses[1].title != "arch.png" {
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

	if same, uses, _ := applyEmbeds(body, nil); same != body || uses != nil {
		t.Error("without embeds the body must not change")
	}
}

func TestLoadEmbeds(t *testing.T) {
	dir := t.TempDir()
	home, _ := os.UserHomeDir()
	path := filepath.Join(dir, "embeds.yaml")
	os.WriteFile(path, []byte("- url: ID1\n  file: ~/a.pdf\n- url: ID2\n  file: rel/b.pdf\n- url: ID3\n  file: /abs/c.pdf\n"), 0o644)
	embeds, err := LoadEmbeds(path, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(home, "a.pdf"), filepath.Join(dir, "rel", "b.pdf"), "/abs/c.pdf"}
	for i, e := range embeds {
		if e.File != want[i] {
			t.Errorf("entry %d: %q, want %q", i, e.File, want[i])
		}
	}
	if e, err := LoadEmbeds(filepath.Join(dir, "none.yaml"), false); err != nil || e != nil {
		t.Errorf("a missing default file is no error: %v", err)
	}
	if _, err := LoadEmbeds(filepath.Join(dir, "none.yaml"), true); err == nil {
		t.Error("a missing named file is an error")
	}
	os.WriteFile(path, []byte("- url: ID1\n"), 0o644)
	if _, err := LoadEmbeds(path, true); err == nil {
		t.Error("an entry without a file is an error")
	}
}

func TestExportAttachesEmbeds(t *testing.T) {
	pdf := []byte("%PDF-1.4 deck")
	docs := t.TempDir()
	file := filepath.Join(docs, "Intro deck.pdf")
	os.WriteFile(file, pdf, 0o644)

	body := `<p>Slides:</p>` + widget(slidesURL) + widget("https://unmapped.example/x")
	cm := http.NewServeMux()
	cm.HandleFunc("GET /rest/api/content/search", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{"results":[],"_links":{}}`) })
	cm.HandleFunc("GET /rest/api/content/{id}/child/attachment", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"results":[],"_links":{}}`)
	})
	cm.HandleFunc("GET /rest/api/content/{id}", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"30","type":"page","title":"Intro","space":{"key":"DEV"},"version":{"number":1,"when":"2026-10-30T10:00:00.000Z"},`+
			`"body":{"storage":{"value":`+jsonString(body)+`}}}`)
	})
	csrv := httptest.NewServer(cm)
	t.Cleanup(csrv.Close)
	ccfg := &config.AtlassianConfig{URL: csrv.URL, Token: "x"}
	e := &Exporter{Confluence: confluence.NewClient(ccfg), ConfCfg: ccfg}
	out := filepath.Join(t.TempDir(), "m.tar.gz")
	var log bytes.Buffer
	if _, err := e.Export(context.Background(), ExportOptions{
		Out: out, Log: &log, Pages: []string{"30"}, Embeds: []Embed{{URL: "1jwmHV7I", File: file}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "https://unmapped.example/x: no local copy mapped") {
		t.Errorf("the unmapped widget must be reported:\n%s", log.String())
	}

	dir := t.TempDir()
	if _, err := Import(out, dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := mirror.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	msrv := httptest.NewServer(s) // as mirror serve
	t.Cleanup(msrv.Close)
	s.ServeAt(msrv.URL)
	c := confluence.NewClient(&config.AtlassianConfig{URL: msrv.URL + "/wiki", Token: "x"})
	p, err := c.GetPage(context.Background(), "30", []string{"body.storage"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body.Storage.Value, `<ac:structured-macro ac:name="viewpdf"><ac:parameter ac:name="name"><ri:attachment ri:filename="Intro deck.pdf" />`) {
		t.Errorf("body: %s", p.Body.Storage.Value)
	}
	atts, err := c.GetAttachments(context.Background(), "30")
	if err != nil || len(atts) != 1 || atts[0].Title != "Intro deck.pdf" || atts[0].ID != embedID(pdf) {
		t.Fatalf("attachments: %+v, %v", atts, err)
	}
	rc, err := c.DownloadAttachment(context.Background(), atts[0].Links["download"])
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if got, _ := io.ReadAll(rc); !bytes.Equal(got, pdf) {
		t.Errorf("downloaded %q", got)
	}
}

func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
