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
	"github.com/zhani/atlassian-cli/internal/embeds"
	"github.com/zhani/atlassian-cli/internal/mirror"
)

const slidesURL = "https://docs.google.com/presentation/d/1jwmHV7I/edit?usp=sharing"

func widget(u string) string {
	return `<ac:structured-macro ac:name="widget" ac:schema-version="1"><ac:parameter ac:name="width">1400</ac:parameter>` +
		`<ac:parameter ac:name="url"><ri:url ri:value="` + u + `" /></ac:parameter></ac:structured-macro>`
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
		Out: out, Log: &log, Pages: []string{"30"}, Embeds: []embeds.Embed{{URL: "1jwmHV7I", File: file}},
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
